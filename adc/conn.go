package adc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/direct-connect/go-dc/lineproto"
)

var (
	Debug bool
)

type Route interface {
	WriteMessage(msg Message) error
	Flush() error
}

var dialer = net.Dialer{}

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
	return DialContext(context.Background(), addr)
}

// DialContext connects to a specified address.
func DialContext(ctx context.Context, addr string) (*Conn, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		u, err = url.Parse("adc://" + addr)
		if err != nil {
			return nil, err
		}
	}
	secure := false
	switch u.Scheme {
	case SchemaADC:
		// continue
	case SchemaADCS:
		secure = true
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", u.Scheme)
	}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if secure {
		sconn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
		})
		if err = sconn.Handshake(); err != nil {
			return nil, fmt.Errorf("TLS handshake failed: %v", err)
		}
		conn = sconn
	}
	return NewConn(conn)
}

// NewConn runs an ADC protocol over a specified connection.
func NewConn(conn net.Conn) (*Conn, error) {
	c := &Conn{
		conn: conn,
	}
	c.write.w = bufio.NewWriter(conn)
	c.read = lineproto.NewReader(conn, 0x0a)
	if Debug {
		c.read.OnLine = func(line []byte) (bool, error) {
			line = bytes.TrimSuffix(line, []byte{0x0a})
			log.Println("<-", string(line))
			return true, nil
		}
	}
	return c, nil
}

// Conn is an ADC protocol connection.
type Conn struct {
	closed chan struct{}

	// bin should be acquired as RLock on commands read/write
	// and as Lock when switching to binary mode.
	bin sync.RWMutex

	conn net.Conn

	write struct {
		sync.Mutex
		err error
		w   *bufio.Writer
	}
	read *lineproto.Reader
}

func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// Close closes the connection.
func (c *Conn) Close() error {
	if c.closed != nil {
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
	}
	return c.conn.Close()
}

// KeepAlive starts sending keep-alive messages on the connection.
func (c *Conn) KeepAlive(interval time.Duration) {
	if c.closed != nil {
		// already enabled
		return
	}
	c.closed = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
			}
			// empty packet serves as keep-alive for ADC
			err := c.writeRawPacket(nil)
			if err == nil {
				err = c.Flush()
			}
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}()
}

// ReadPacket reads and decodes a single ADC command.
func (c *Conn) ReadPacket(deadline time.Time) (Packet, error) {
	p, err := c.readPacket(deadline)
	if err != nil {
		return nil, err
	}
	return DecodePacket(p)
}

// readPacket reads a single ADC packet (separated by 0x0a byte) without decoding it.
func (c *Conn) readPacket(deadline time.Time) ([]byte, error) {
	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	for {
		s, err := c.read.ReadLine()
		if err != nil {
			return nil, err
		}
		// trim delimiter
		s = s[:len(s)-1]
		if len(s) != 0 {
			return s, nil
		}
		// clients may send message containing only 0x0a byte
		// to keep connection alive - we should ignore these messages
	}
}

func (c *Conn) ReadInfoMsg(deadline time.Time) (Message, error) {
	cmd, err := c.ReadPacket(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok := cmd.(*InfoPacket)
	if !ok {
		return nil, fmt.Errorf("expected info command, got: %#v", cmd)
	}
	return UnmarshalMessage(cc.Name, cc.Data)
}

func (c *Conn) ReadClientMsg(deadline time.Time) (Message, error) {
	cmd, err := c.ReadPacket(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok := cmd.(*ClientPacket)
	if !ok {
		return nil, fmt.Errorf("expected client command, got: %#v", cmd)
	}
	return UnmarshalMessage(cc.Name, cc.Data)
}

func (c *Conn) Broadcast(from SID) Route {
	return broadcastRoute{c: c, sid: from}
}

type broadcastRoute struct {
	c   *Conn
	sid SID
}

func (r broadcastRoute) WriteMessage(msg Message) error {
	return r.c.WriteBroadcast(r.sid, msg)
}

func (r broadcastRoute) Flush() error {
	return r.c.Flush()
}

func (c *Conn) Direct(from, to SID) Route {
	return directRoute{c: c, sid: from, targ: to}
}

type directRoute struct {
	c    *Conn
	sid  SID
	targ SID
}

func (r directRoute) WriteMessage(msg Message) error {
	return r.c.WriteDirect(r.sid, r.targ, msg)
}

func (r directRoute) Flush() error {
	return r.c.Flush()
}

func (c *Conn) WriteInfoMsg(msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&InfoPacket{
		BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WriteHubMsg(msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&HubPacket{
		BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WriteClientMsg(msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&ClientPacket{
		BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WriteBroadcast(id SID, msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&BroadcastPacket{
		ID:         id,
		BasePacket: BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WriteDirect(id, targ SID, msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&DirectPacket{
		ID: id, Targ: targ,
		BasePacket: BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WriteEcho(id, targ SID, msg Message) error {
	data, err := Marshal(msg)
	if err != nil {
		return err
	}
	return c.WritePacket(&EchoPacket{
		ID: id, Targ: targ,
		BasePacket: BasePacket{Name: msg.Cmd(), Data: data},
	})
}

func (c *Conn) WritePacket(p Packet) error {
	data, err := p.MarshalPacket()
	if err != nil {
		return err
	}
	return c.writeRawPacket(data)
}

func (c *Conn) writeRawPacket(s []byte) error {
	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.write.Lock()
	defer c.write.Unlock()

	if err := c.write.err; err != nil {
		return err
	}
	if Debug {
		log.Println("->", string(s))
	}
	_, err := c.write.w.Write(s)
	if err != nil {
		c.write.err = err
	}
	err = c.write.w.WriteByte(0x0a)
	if err != nil {
		c.write.err = err
	}
	return err
}

// Flush the underlying buffer. Should be called after each WritePacket batch.
func (c *Conn) Flush() error {
	if Debug {
		log.Println("-> [flushed]")
	}

	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.write.Lock()
	defer c.write.Unlock()

	if err := c.write.err; err != nil {
		return err
	}

	err := c.write.w.Flush()
	if err != nil {
		c.write.err = err
	}
	return err
}

/*
func (c *Conn) HubHandshake(f ModFeatures, sid SID, info HubInfo) (*User, error) {
	const initialWait = time.Second * 5
	// Read supported features from the client
	cmd, err := c.ReadPacket(initialWait)
	if err != nil {
		return nil, err
	} else if cmd.Kind() != kindHub || cmd.GetName() != CmdSupport {
		return nil, fmt.Errorf("expected SUP command, got: %v, %v", cmd.Kind(), cmd.GetName())
	}
	f = f.SetFrom(supExtensionsHub2Cli)
	var fea = make(ModFeatures)
	if err = Unmarshal(string(cmd.Data()), &fea); err != nil {
		return nil, err
	}
	c.fea = f.Intersect(fea)
	if Debug {
		log.Printf("features: %v, mutual: %v\n", fea, c.fea)
	}
	if !c.fea.IsSet(FeaTIGR) {
		return nil, fmt.Errorf("client has no support for TIGR")
	} else if !c.fea.IsSet(FeaBASE) {
		return nil, fmt.Errorf("client has no support for BASE")
	}
	//c.zlibGet = c.fea[extZLIG]

	// Send ours supported features
	c.WritePacket(NewInfoCmd(CmdSupport, f))
	// Assign SID to the client
	c.WritePacket(NewInfoCmd(CmdSession, sid))
	// Send hub info
	c.WritePacket(NewInfoCmd(CmdInfo, info))
	// Write OK status
	c.WritePacket(NewInfoCmd(CmdStatus, Status{Msg: "Powered by Gophers"}))
	if err := c.Flush(); err != nil {
		return nil, err
	}
	cmd, err = c.ReadPacket(initialWait)
	if err != nil {
		return nil, err
	}
	ucmd, ok := cmd.(BroadcastPacket)
	if !ok || ucmd.Name != CmdInfo {
		return nil, fmt.Errorf("expected INF command, got: %v, %v", cmd.Kind(), cmd.GetName())
	}
	var user User
	if err = Unmarshal(string(ucmd.Data), &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user info: %v", err)
	}
	return &user, nil
}

func (c *Conn) ClientHandshake(toHub bool, f ModFeatures) error { // TODO: ctx
	if len(f) == 0 {
		if toHub {
			f = supExtensionsCli2Hub.Clone()
		} else {
			f = supExtensionsCli2Cli.Clone()
		}
	}
	if _, ok := f[extZLIG]; !ok {
		f[extZLIG] = UseZLibGet
	}
	// Send supported features and initiate state machine
	if toHub {
		c.WritePacket(NewHubCmd(CmdSupport, f))
	} else {
		c.WritePacket(NewClientCmd(CmdSupport, f))
	}
	if err := c.Flush(); err != nil {
		return err
	}
	const protocolWait = time.Second * 5
	for {
		cmd, err := c.ReadPacket(protocolWait)
		if err != nil {
			return err
		} else if (toHub && cmd.Kind() != kindInfo) || (!toHub && cmd.Kind() != kindClient) {
			return fmt.Errorf("unexpected command kind in Protocol, got: %v", cmd)
		}
		switch cmd.GetName() {
		case CmdStatus:
			fmt.Println("got status in Protocol:", cmd)
		case CmdSupport: // Expect to get supported extensions back
			var fea = make(ModFeatures)
			Unmarshal(string(cmd.Data()), &fea)
			c.fea = f.Intersect(fea)
			fmt.Printf("features: %v, mutual: %v\n", fea, c.fea)
			if !c.fea.IsSet(FeaTIGR) {
				return fmt.Errorf("peer has no support for TIGR")
			} else if !c.fea.IsSet(FeaBASE) && !c.fea.IsSet("BAS0") {
				return fmt.Errorf("peer has no support for BASE")
			}
			c.zlibGet = c.fea[extZLIG]
			if !toHub {
				return nil
			}
		case CmdSession: // Hub must send a SID for us
			if !toHub {
				return fmt.Errorf("unexpected session in C-C connection: %v", cmd)
			} else if len(cmd.Data()) != 4 {
				return fmt.Errorf("wrong SID format: '%s'", cmd.Data())
			}
			if err := c.sid.UnmarshalAdc(string(cmd.Data())); err != nil {
				return err
			}
			fmt.Println("SID:", c.sid)
			return nil
		default:
			return fmt.Errorf("unexpected command in Protocol: %v", cmd)
		}
	}
}
*/

// ReadBinary acquires an exclusive reader lock on the connection and switches it to binary mode.
// Reader will be limited to exactly n bytes. Unread content will be discarded on close.
//func (c *Conn) ReadBinary(n int64) io.ReadCloser {
//	if n <= 0 {
//		return ioutil.NopCloser(bytes.NewReader(nil))
//	}
//	// acquire exclusive connection lock
//	c.bin.Lock()
//	if Debug {
//		log.Printf("<- [binary data: %d bytes]", n)
//	}
//	return &rawReader{c: c, r: io.LimitReader(c.read.r, n)}
//}

type rawReader struct {
	c   *Conn
	r   io.Reader
	err error
}

func (r *rawReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.r.Read(p)
	if err != nil {
		r.err = err
		r.close()
	}
	return n, err
}

func (r *rawReader) close() {
	if r.err == nil {
		r.err = io.EOF
	}
	// drain remaining data
	_, _ = io.Copy(ioutil.Discard, r.r)
	// unlock the connection
	r.c.bin.Unlock()
	r.c = nil
	if Debug {
		log.Println("<- [binary end]")
	}
}

func (r *rawReader) Close() error {
	if r.err == io.EOF {
		return nil
	} else if r.err != nil {
		return r.err
	}
	r.close()
	return nil
}
