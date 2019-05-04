package nmdc

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"

	"github.com/direct-connect/go-dc/nmdc"
)

var (
	Debug bool

	DefaultFallbackEncoding encoding.Encoding
)

const writeBuffer = 0

var dialer = net.Dialer{}

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
	return DialContext(context.Background(), addr)
}

// DialContext connects to a specified address.
func DialContext(ctx context.Context, addr string) (*Conn, error) {
	u, err := nmdc.ParseAddr(addr)
	if err != nil {
		return nil, err
	}

	secure := false
	switch u.Scheme {
	case nmdc.SchemeNMDC:
		// continue
	case nmdc.SchemeNMDCS:
		secure = true
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", u.Scheme)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		var err2 error
		host, port, err2 = net.SplitHostPort(u.Host + ":" + strconv.Itoa(nmdc.DefaultPort))
		if err2 != nil {
			return nil, err
		}
	}
	u.Host = net.JoinHostPort(host, port)

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

// NewConn runs an NMDC protocol over a specified connection.
func NewConn(conn net.Conn) (*Conn, error) {
	c := &Conn{
		conn: conn,
	}
	c.w = nmdc.NewWriterSize(conn, writeBuffer)
	c.r = nmdc.NewReader(conn)
	c.r.OnUnknownEncoding = c.onUnknownEncoding
	if DefaultFallbackEncoding != nil {
		c.SetFallbackEncoding(DefaultFallbackEncoding)
	}
	c.r.OnRawMessage(func(cmd, args []byte) (bool, error) {
		if bytes.Equal(cmd, []byte("ZOn")) {
			err := c.r.EnableZlib()
			return false, err
		}
		return true, nil
	})
	if Debug {
		c.w.OnLine(func(line []byte) (bool, error) {
			log.Printf("-> %q", string(line))
			return true, nil
		})
		c.r.OnLine(func(line []byte) (bool, error) {
			log.Printf("<- %q", string(line))
			return true, nil
		})
	}
	return c, nil
}

// Conn is a NMDC protocol connection.
type Conn struct {
	cmu    sync.Mutex
	closed bool

	fallback encoding.Encoding

	conn net.Conn

	w *nmdc.Writer
	r *nmdc.Reader
}

func (c *Conn) OnUnmarshalError(fnc func(line []byte, err error) (bool, error)) {
	c.r.OnUnmarshalError = fnc
}

func (c *Conn) OnLineR(fnc func(line []byte) (bool, error)) {
	c.r.OnLine(fnc)
}

func (c *Conn) OnLineW(fnc func(line []byte) (bool, error)) {
	c.w.OnLine(fnc)
}

func (c *Conn) OnRawMessageR(fnc func(cmd, data []byte) (bool, error)) {
	c.r.OnRawMessage(fnc)
}

func (c *Conn) OnMessageR(fnc func(m nmdc.Message) (bool, error)) {
	c.r.OnMessage(fnc)
}

func (c *Conn) OnMessageW(fnc func(m nmdc.Message) (bool, error)) {
	c.w.OnMessage(fnc)
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

func (c *Conn) FallbackEncoding() encoding.Encoding {
	return c.fallback
}

func (c *Conn) TextEncoder() *encoding.Encoder {
	return c.w.Encoder()
}

func (c *Conn) TextDecoder() *encoding.Decoder {
	return c.r.Decoder()
}

func (c *Conn) setEncoding(enc encoding.Encoding, event bool) {
	if enc != nil {
		e := enc.NewEncoder()
		e = encoding.HTMLEscapeUnsupported(e)
		c.w.SetEncoder(e)
		if !event {
			c.r.SetDecoder(enc.NewDecoder())
		}
	} else {
		c.w.SetEncoder(nil)
		if !event {
			c.r.SetDecoder(nil)
		}
	}
}

func (c *Conn) SetEncoding(enc encoding.Encoding) {
	c.setEncoding(enc, false)
}

func (c *Conn) SetFallbackEncoding(enc encoding.Encoding) {
	c.fallback = enc
}

func (c *Conn) ZOn() error {
	return c.w.ZOn()
}

// Close closes the connection.
func (c *Conn) Close() error {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	// should not hold any other mutex
	return c.conn.Close()
}

func (c *Conn) WriteMsg(m ...nmdc.Message) error {
	return c.w.WriteMsg(m...)
}

func (c *Conn) WriteLine(data []byte) error {
	return c.w.WriteLine(data)
}

func (c *Conn) Flush() error {
	return c.w.Flush()
}

func (c *Conn) WriteOneMsg(m nmdc.Message) error {
	if err := c.w.WriteMsg(m); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Conn) WriteOneLine(data []byte) error {
	if err := c.w.WriteLine(data); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Conn) onUnknownEncoding(text []byte) (*encoding.Decoder, error) {
	fallback := c.FallbackEncoding()
	if fallback == nil {
		return nil, nil
	}
	// try fallback encoding
	dec := fallback.NewDecoder()
	str, err := dec.String(string(text))
	if err != nil || !utf8.ValidString(str) {
		return nil, nil // use current decoder
	}
	// fallback is valid - switch encoding
	if Debug {
		log.Println(c.RemoteAddr(), "switched to a fallback encoding")
	}
	c.setEncoding(fallback, true)
	return dec, nil
}

func (c *Conn) ReadMsgTo(deadline time.Time, m nmdc.Message) error {
	if m == nil {
		panic("nil message to decode")
	}
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadMsgTo(m)
}

func (c *Conn) ReadMsgToAny(deadline time.Time, m ...nmdc.Message) (nmdc.Message, error) {
	if len(m) == 0 {
		panic("no messages to decode")
	}
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadMsgToAny(m...)
}

func (c *Conn) ReadMsg(deadline time.Time) (nmdc.Message, error) {
	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.r.ReadMsg()
}
