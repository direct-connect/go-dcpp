package adc

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/direct-connect/go-dc/adc"
)

var (
	Debug bool
)

const writeBuffer = 0

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
	case adc.SchemaADC:
		// continue
	case adc.SchemaADCS:
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
			_ = sconn.Close()
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
	c.w = adc.NewWriterSize(conn, writeBuffer)
	c.r = adc.NewReader(conn)
	if Debug {
		c.w.OnLine(func(line []byte) (bool, error) {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			log.Println("->", string(line))
			return true, nil
		})
		c.r.OnLine(func(line []byte) (bool, error) {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			log.Println("<-", string(line))
			return true, nil
		})
	}
	return c, nil
}

// Conn is an ADC protocol connection.
type Conn struct {
	closed chan struct{}

	conn net.Conn

	w *adc.Writer
	r *adc.Reader
}

func (c *Conn) OnLineR(fnc func(line []byte) (bool, error)) {
	c.r.OnLine(fnc)
}

func (c *Conn) OnLineW(fnc func(line []byte) (bool, error)) {
	c.w.OnLine(fnc)
}

func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *Conn) SetWriteTimeout(dt time.Duration) {
	if dt <= 0 {
		c.w.Timeout = nil
		return
	}
	c.w.Timeout = func(enable bool) error {
		if enable {
			return c.conn.SetWriteDeadline(time.Now().Add(dt))
		}
		return c.conn.SetWriteDeadline(time.Time{})
	}
}

func (c *Conn) ZOn(lvl int) error {
	return c.w.EnableZlibLevel(lvl)
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

func (c *Conn) WriteKeepAlive() error {
	return c.w.WriteKeepAlive()
}

// ReadPacket reads and decodes a single ADC command.
func (c *Conn) ReadPacket(deadline time.Time) (adc.Packet, error) {
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadPacket()
}

// ReadPacketRaw reads and decodes a single ADC command. Caller must copy the payload.
func (c *Conn) ReadPacketRaw(deadline time.Time) (adc.Packet, error) {
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadPacketRaw()
}

func (c *Conn) ReadInfoMsg(deadline time.Time) (adc.Message, error) {
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadInfo()
}

func (c *Conn) ReadClientMsg(deadline time.Time) (adc.Message, error) {
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadClient()
}

func (c *Conn) Broadcast(from SID) adc.WriteStream {
	return c.w.Broadcast(from)
}

func (c *Conn) WriteInfoMsg(msg adc.Message) error {
	return c.w.WriteInfoMsg(msg)
}

func (c *Conn) WriteHubMsg(msg adc.Message) error {
	return c.w.WriteHubMsg(msg)
}

func (c *Conn) WriteClientMsg(msg adc.Message) error {
	return c.w.WriteClientMsg(msg)
}

func (c *Conn) WriteBroadcast(id SID, msg adc.Message) error {
	return c.w.WriteBroadcast(id, msg)
}

func (c *Conn) WriteDirect(id, targ SID, msg adc.Message) error {
	return c.w.WriteDirect(id, targ, msg)
}

func (c *Conn) WriteEcho(id, targ SID, msg adc.Message) error {
	return c.w.WriteEcho(id, targ, msg)
}

func (c *Conn) WritePacket(p adc.Packet) error {
	return c.w.WritePacket(p)
}

// Flush the underlying buffer. Should be called after each WritePacket batch.
func (c *Conn) Flush() error {
	return c.w.Flush()
}
