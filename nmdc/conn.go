package nmdc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	SchemaNMDC  = "dchub://"
	DefaultPort = 411
)

const (
	readBuf     = 4096
	maxUserName = 256
	maxCmdName  = 64
	maxChatMsg  = readBuf
	maxCmd      = readBuf * 4
)

var Debug bool

func ParseAddr(addr string) (*url.URL, error) {
	if !strings.Contains(addr, "://") {
		addr = SchemaNMDC + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if u.Scheme+"://" != SchemaNMDC {
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

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
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

	conn, err := net.Dial("tcp", net.JoinHostPort(host, port))
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
	c.read.r = conn
	c.read.buf = make([]byte, 0, readBuf)
	return c, nil
}

// Conn is a NMDC protocol connection.
type Conn struct {
	cmu    sync.Mutex
	closed chan struct{}

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
		err error
		buf []byte
		i   int
		r   io.Reader
	}
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
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
func (c *Conn) WriteMsg(m Message) error {
	data, err := Marshal(m)
	if err != nil {
		return err
	}
	return c.WriteRaw(data)
}

// WriteOneMsg writes a single message to the connection's buffer
// and schedules the write.
func (c *Conn) WriteOneMsg(m Message) error {
	data, err := Marshal(m)
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

func (c *Conn) peek() ([]byte, error) {
	if c.read.i < len(c.read.buf) {
		return c.read.buf[c.read.i:], nil
	}
	c.read.i = 0
	c.read.buf = c.read.buf[:cap(c.read.buf)]
	n, err := c.read.r.Read(c.read.buf)
	if err != nil {
		select {
		case <-c.closed:
			err = io.EOF
		default:
		}
	}
	c.read.buf = c.read.buf[:n]
	return c.read.buf, err
}

func (c *Conn) discard(n int) {
	if n < 0 {
		c.read.i += len(c.read.buf)
	} else {
		c.read.i += n
	}
}

func (c *Conn) readMsgTo(deadline time.Time, ptr *Message) error {
	defer c.readMsgLock()()

	if err := c.read.err; err != nil {
		return err
	}

	if !deadline.IsZero() {
		c.conn.SetReadDeadline(deadline)
		defer c.conn.SetReadDeadline(time.Time{})
	}

	for {
		b, err := c.peek()
		if err != nil {
			return err
		}
		if len(b) == 1 && b[0] == '|' {
			c.discard(1)
			continue // keep alive
		}
		msg := *ptr
		if b[0] != '$' {
			// not a command - chat message
			m, ok := msg.(*ChatMessage)
			if !ok {
				if msg != nil {
					return errors.New("expected chat message, got command")
				}
				m = &ChatMessage{}
				*ptr = m
			}
			return c.readChatMsg(m)
		}
		// command
		raw, err := c.readRawCommand()
		if err != nil {
			return err
		}
		if msg == nil {
			m, err := raw.Decode()
			if err != nil {
				return err
			}
			*ptr = m
			return nil
		}
		if msg.Cmd() != raw.Name {
			return fmt.Errorf("expected %q, got %q", msg.Cmd(), raw.Name)
		}
		return msg.UnmarshalNMDC(raw.Data)
	}
}

func (c *Conn) ReadMsgTo(deadline time.Time, m Message) error {
	if m == nil {
		panic("nil message to decode")
	}
	return c.readMsgTo(deadline, &m)
}

func (c *Conn) ReadMsg(deadline time.Time) (Message, error) {
	var m Message
	if err := c.readMsgTo(deadline, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// readUntilAny reads a byte slice until one of the char delimiters, up to max bytes.
// It returns a slice with a delimiter and reads the delimiter from the connection.
func (c *Conn) readUntilAny(chars string, max int) ([]byte, error) {
	var value []byte
	for {
		b, err := c.peek()
		if err != nil {
			return nil, err
		}
		i := bytes.IndexAny(b, chars)
		if i >= 0 {
			value = append(value, b[:i+1]...)
			c.discard(i + 1)
			return value, nil
		}
		if len(value)+len(b) > max {
			return nil, errors.New("value is too large")
		}
		value = append(value, b...)
		c.discard(-1)
	}
}

func (c *Conn) readChatMsg(m *ChatMessage) error {
	*m = ChatMessage{}
	// <Bob> hello|
	// or
	// Some info|

	b, err := c.peek()
	if err != nil {
		return err
	}
	if b[0] == '<' {
		c.discard(1) // trim '<'
		name, err := c.readUntilAny(">", maxUserName)
		if err != nil {
			return fmt.Errorf("cannot read username in chat message: %v", err)
		}
		name = name[:len(name)-1] // trim '>'
		if len(name) == 0 {
			return errors.New("empty name in chat message")
		}
		if err = m.Name.UnmarshalNMDC(name); err != nil {
			return err
		}

		b, err = c.peek()
		if err != nil {
			return err
		}
		if len(b) < 1 || b[0] != ' ' {
			return errors.New("cannot parse chat message")
		}
		c.discard(1) // discard ' '
	}

	msg, err := c.readUntilAny("|", maxChatMsg)
	if err != nil {
		return fmt.Errorf("cannot read chat message: %v", err)
	}
	msg = msg[:len(msg)-1] // trim '|'
	// TODO: convert to UTF8
	if err = m.Text.UnmarshalNMDC(msg); err != nil {
		return err
	}
	if Debug {
		data, _ := m.MarshalNMDC()
		log.Println("<-", string(data))
	}
	return nil
}

func (c *Conn) readRawCommand() (*RawCommand, error) {
	// $Name xxx yyy|
	c.discard(1) // trim '$'

	buf, err := c.readUntilAny("|", maxCmd)
	if err == io.EOF {
		return nil, io.EOF
	} else if err != nil {
		return nil, fmt.Errorf("cannot parse command: %v", err)
	}
	if Debug {
		log.Println("<-", "$"+string(buf))
	}
	buf = buf[:len(buf)-1] // trim '|'

	i := bytes.Index(buf, []byte(" "))
	if i < 0 {
		return &RawCommand{Name: string(buf)}, nil
	}
	return &RawCommand{Name: string(buf[:i]), Data: buf[i+1:]}, nil
}

func (c *Conn) readCommand() (Message, error) {
	raw, err := c.readRawCommand()
	if err != nil {
		return nil, err
	}
	return raw.Decode()
}
