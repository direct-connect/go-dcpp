package nmdc

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"

	"github.com/direct-connect/go-dc/nmdc"
)

const (
	SchemaNMDC  = "dchub"
	DefaultPort = 411
)

const (
	invalidCharsName = "$\x00\r\n\t"
)

var Debug bool

func ParseAddr(addr string) (*url.URL, error) {
	if !strings.Contains(addr, "://") {
		addr = SchemaNMDC + "://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if u.Scheme != SchemaNMDC {
		return u, fmt.Errorf("unsupported protocol: %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func NormalizeAddr(addr string) (string, error) {
	u, err := ParseAddr(addr)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

var dialer = net.Dialer{}

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
	return DialContext(context.Background(), addr)
}

// DialContext connects to a specified address.
func DialContext(ctx context.Context, addr string) (*Conn, error) {
	u, err := ParseAddr(addr)
	if err != nil {
		return nil, err
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		var err2 error
		host, port, err2 = net.SplitHostPort(u.Host + ":" + strconv.Itoa(DefaultPort))
		if err2 != nil {
			return nil, err
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	return NewConn(conn)
}

// NewConn runs an NMDC protocol over a specified connection.
func NewConn(conn net.Conn) (*Conn, error) {
	c := &Conn{
		conn:   conn,
		closed: make(chan struct{}),
	}
	c.write.w = bufio.NewWriter(conn)
	c.read.r = nmdc.NewReader(conn)
	c.read.r.OnUnknownEncoding = c.onUnknownEncoding
	if Debug {
		c.read.r.OnLine = func(data []byte) (bool, error) {
			log.Println("<-", string(data))
			return true, nil
		}
	}
	return c, nil
}

// Conn is a NMDC protocol connection.
type Conn struct {
	cmu    sync.Mutex
	closed chan struct{}

	encoding encoding.Encoding
	fallback encoding.Encoding
	enc      *encoding.Encoder
	dec      *encoding.Decoder

	// bin should be acquired as RLock on commands read/write
	// and as Lock when switching to binary mode.
	bin sync.RWMutex

	conn net.Conn

	write struct {
		active     int32 // atomic
		schedule   chan<- struct{}
		unschedule chan<- struct{}

		sync.Mutex
		err error
		w   *bufio.Writer
	}
	read struct {
		sync.Mutex
		r *nmdc.Reader
	}
}

func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Conn) Encoding() encoding.Encoding {
	return c.encoding
}

func (c *Conn) TextEncoder() *encoding.Encoder {
	return c.enc
}

func (c *Conn) TextDecoder() *encoding.Decoder {
	return c.dec
}

func (c *Conn) setEncoding(enc encoding.Encoding) {
	c.encoding = enc
	if enc != nil {
		c.enc = enc.NewEncoder()
		c.dec = enc.NewDecoder()
	} else {
		c.enc = nil
		c.dec = nil
	}
	c.read.r.SetDecoder(c.dec)
}

func (c *Conn) SetEncoding(enc encoding.Encoding) {
	c.read.Lock()
	defer c.read.Unlock()
	c.write.Lock()
	defer c.write.Unlock()
	c.setEncoding(enc)
}

func (c *Conn) SetFallbackEncoding(enc encoding.Encoding) {
	c.fallback = enc
}

// Close closes the connection.
func (c *Conn) Close() error {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	// should not hold any other mutex
	select {
	case <-c.closed:
		return nil
	default:
		close(c.closed)
	}
	return c.conn.Close()
}

// KeepAlive starts sending keep-alive messages on the connection.
func (c *Conn) KeepAlive(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// skip one tick
		select {
		case <-c.closed:
			return
		case <-ticker.C:
		}
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
			}
			// empty message serves as keep-alive for NMDC
			err := c.WriteOneRaw([]byte("|"))
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}()
}

// WriteMsg writes a single message to the connection's buffer.
// Caller should call Flush or FlushLazy to schedule the write.
func (c *Conn) WriteMsg(m nmdc.Message) error {
	data, err := nmdc.Marshal(c.enc, m)
	if err != nil {
		return err
	}
	return c.WriteRaw(data)
}

// WriteOneMsg writes a single message to the connection's buffer
// and schedules the write.
func (c *Conn) WriteOneMsg(m nmdc.Message) error {
	data, err := nmdc.Marshal(c.enc, m)
	if err != nil {
		return err
	}
	return c.WriteOneRaw(data)
}

func (c *Conn) Flush() error {
	defer c.writeMsgLock()()

	return c.flushUnsafe()
}

func (c *Conn) writeRawUnsafe(data []byte) error {
	if err := c.write.err; err != nil {
		return err
	}
	if Debug {
		log.Println("->", string(data))
	}
	_, err := c.write.w.Write(data)
	if err != nil {
		c.write.err = err
		_ = c.Close()
	}
	return err
}

func (c *Conn) flushUnsafe() error {
	if c.write.schedule != nil {
		close(c.write.unschedule)
		c.write.schedule = nil
		c.write.unschedule = nil
	}
	if err := c.write.err; err != nil {
		return err
	}
	err := c.write.w.Flush()
	if err != nil {
		c.write.err = err
		_ = c.Close()
	}
	if Debug {
		log.Println("-> [flushed]")
	}
	return err
}

func (c *Conn) scheduleFlush() error {
	if err := c.write.err; err != nil {
		return err
	}
	if c.write.schedule != nil {
		select {
		case c.write.schedule <- struct{}{}:
		default:
		}
		return nil
	}
	const delay = time.Millisecond * 15
	re := make(chan struct{}, 1)
	un := make(chan struct{})
	c.write.schedule = re
	c.write.unschedule = un
	timer := time.NewTimer(delay)
	go func() {
		defer timer.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-un:
				return
			case <-timer.C:
				_ = c.Flush()
				return
			case <-re:
			}
		}
	}()
	return nil
}

func (c *Conn) writeMsgLock() func() {
	atomic.AddInt32(&c.write.active, 1)
	// make sure connection is not in binary mode
	c.bin.RLock()
	c.write.Lock()
	return func() {
		atomic.AddInt32(&c.write.active, -1)
		c.write.Unlock()
		c.bin.RUnlock()
	}
}

func (c *Conn) readMsgLock() func() {
	// make sure connection is not in binary mode
	c.bin.RLock()
	c.read.Lock()
	return func() {
		c.read.Unlock()
		c.bin.RUnlock()
	}
}

func (c *Conn) WriteRaw(data []byte) error {
	defer c.writeMsgLock()()

	return c.writeRawUnsafe(data)
}

func (c *Conn) WriteOneRaw(data []byte) error {
	defer c.writeMsgLock()()

	if err := c.writeRawUnsafe(data); err != nil {
		return err
	}
	if atomic.LoadInt32(&c.write.active) > 1 {
		// next one will flush
		return nil
	}
	return c.scheduleFlush()
}

func (c *Conn) onUnknownEncoding(text []byte) (*encoding.Decoder, error) {
	if c.fallback == nil {
		return nil, nil
	}
	// try fallback encoding
	dec := c.fallback.NewDecoder()
	str, err := dec.String(string(text))
	if err != nil || !utf8.ValidString(str) {
		return nil, nil // use current decoder
	}
	// fallback is valid - switch encoding

	// already holding read lock, only need to acquire write lock
	c.write.Lock()
	c.setEncoding(c.fallback)
	c.write.Unlock()

	return dec, nil
}

func (c *Conn) ReadMsgTo(deadline time.Time, m nmdc.Message) error {
	if m == nil {
		panic("nil message to decode")
	}

	defer c.readMsgLock()()

	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}
	return c.read.r.ReadMsgTo(m)
}

func (c *Conn) ReadMsg(deadline time.Time) (nmdc.Message, error) {
	defer c.readMsgLock()()

	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}

	return c.read.r.ReadMsg()
}
