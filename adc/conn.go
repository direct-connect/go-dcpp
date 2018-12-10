package adc

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"
)

var (
	Debug bool
)

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return NewConn(conn)
}

// NewConn runs an ADC protocol over a specified connection.
func NewConn(conn net.Conn) (*Conn, error) {
	c := &Conn{
		conn: conn,
	}
	c.write.w = bufio.NewWriter(conn)
	c.read.r = bufio.NewReader(conn)
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
	read struct {
		sync.Mutex
		err error
		r   *bufio.Reader
	}
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
			// empty message serves as keep-alive for ADC
			_ = c.WriteMessage(nil)
		}
	}()
}

// ReadCmd reads and decodes a single ADC command.
func (c *Conn) ReadCmd(deadline time.Time) (Command, error) {
	p, err := c.ReadMessage(deadline)
	if err != nil {
		return nil, err
	}
	return DecodeCmd(p)
}

// ReadMessage reads a single ADC message (separated by 0x0a byte) without decoding it.
func (c *Conn) ReadMessage(deadline time.Time) ([]byte, error) {
	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.read.Lock()
	defer c.read.Unlock()

	if err := c.read.err; err != nil {
		return nil, err
	}

	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	for {
		s, err := c.read.r.ReadBytes(byte(0x0a))
		if err == io.EOF {
			if len(s) == 0 {
				c.read.err = err
				return nil, err
			}
			// no delimiter
		} else if err != nil {
			c.read.err = err
			return nil, err
		} else {
			// trim delimiter
			s = s[:len(s)-1]
		}
		if len(s) != 0 {
			if Debug {
				log.Println(">", string(s))
			}
			return s, nil
		}
		// clients may send message containing only 0x0a byte
		// to keep connection alive - we should ignore these messages
	}
}

func (c *Conn) WriteCmd(cmd Command) error {
	s, err := cmd.MarshalAdc()
	if err != nil {
		return err
	}
	return c.WriteMessage(s)
}

func (c *Conn) WriteMessage(s []byte) error {
	if Debug {
		log.Println("<", string(s))
	}

	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.write.Lock()
	defer c.write.Unlock()

	if err := c.write.err; err != nil {
		return err
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

// Flush the underlying buffer. Should be called after each WriteCmd batch.
func (c *Conn) Flush() error {
	if Debug {
		log.Println("< [flushed]")
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
	cmd, err := c.ReadCmd(initialWait)
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
	c.WriteCmd(NewInfoCmd(CmdSupport, f))
	// Assign SID to the client
	c.WriteCmd(NewInfoCmd(CmdSession, sid))
	// Send hub info
	c.WriteCmd(NewInfoCmd(CmdInfo, info))
	// Write OK status
	c.WriteCmd(NewInfoCmd(CmdStatus, Status{Msg: "Powered by Gophers"}))
	if err := c.Flush(); err != nil {
		return nil, err
	}
	cmd, err = c.ReadCmd(initialWait)
	if err != nil {
		return nil, err
	}
	ucmd, ok := cmd.(Broadcast)
	if !ok || ucmd.Name != CmdInfo {
		return nil, fmt.Errorf("expected INF command, got: %v, %v", cmd.Kind(), cmd.GetName())
	}
	var user User
	if err = Unmarshal(string(ucmd.Raw), &user); err != nil {
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
		c.WriteCmd(NewHubCmd(CmdSupport, f))
	} else {
		c.WriteCmd(NewClientCmd(CmdSupport, f))
	}
	if err := c.Flush(); err != nil {
		return err
	}
	const protocolWait = time.Second * 5
	for {
		cmd, err := c.ReadCmd(protocolWait)
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
func (c *Conn) ReadBinary(n int64) io.ReadCloser {
	if n <= 0 {
		return ioutil.NopCloser(bytes.NewReader(nil))
	}
	// acquire exclusive connection lock
	c.bin.Lock()
	return &rawReader{c: c, r: io.LimitReader(c.read.r, n)}
}

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
