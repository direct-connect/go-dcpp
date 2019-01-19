package nmdc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const (
	readBuf     = 4096
	maxUserName = 256
	maxCmdName  = 64
	maxChatMsg  = readBuf
	maxCmd      = readBuf * 4
)

var Debug bool

// TODO: support keep alive with "|"

// Dial connects to a specified address.
func Dial(addr string) (*Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return NewConn(conn)
}

// NewConn runs an NMDC protocol over a specified connection.
func NewConn(conn net.Conn) (*Conn, error) {
	c := &Conn{
		conn: conn,
	}
	c.write.w = bufio.NewWriter(conn)
	c.read.r = conn
	c.read.buf = make([]byte, 0, readBuf)
	return c, nil
}

// Conn is a NMDC protocol connection.
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
	if c.closed != nil {
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
	}
	return c.conn.Close()
}

func (c *Conn) WriteMsg(m Message) error {
	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.write.Lock()
	defer c.write.Unlock()

	if err := c.write.err; err != nil {
		return err
	}

	var (
		data []byte
		err  error
	)
	if cm, ok := m.(*ChatMessage); ok {
		// special case
		data, err = cm.MarshalNMDC()
	} else {
		data, err = m.MarshalNMDC()
		if err == nil {
			name := m.Cmd()
			n := 1 + len(name) + 1
			if len(data) != 0 {
				n += 1 + len(data)
			}
			buf := make([]byte, n)
			i := 0
			buf[i] = '$'
			i++
			i += copy(buf[i:], name)
			if len(data) != 0 {
				buf[i] = ' '
				i++
				i += copy(buf[i:], data)
			}
			buf[i] = '|'
			data = buf
		}
	}
	if err == nil {
		if Debug {
			log.Println("->", string(data))
		}
		_, err = c.write.w.Write(data)
	}
	return err
}

func (c *Conn) Flush() error {
	// make sure connection is not in binary mode
	c.bin.RLock()
	defer c.bin.RUnlock()

	c.write.Lock()
	defer c.write.Unlock()

	if err := c.write.w.Flush(); err != nil {
		return err
	}
	if Debug {
		log.Println("-> [flushed]")
	}
	return nil
}

func (c *Conn) peek() ([]byte, error) {
	if c.read.i < len(c.read.buf) {
		return c.read.buf[c.read.i:], nil
	}
	c.read.i = 0
	c.read.buf = c.read.buf[:cap(c.read.buf)]
	n, err := c.read.r.Read(c.read.buf)
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

func (c *Conn) ReadMsg(deadline time.Time) (Message, error) {
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
		b, err := c.peek()
		if err != nil {
			return nil, err
		}
		if len(b) == 1 && b[0] == '|' {
			continue // keep alive
		}
		if b[0] == '$' {
			return c.readCommand()
		}
		return c.readChatMsg()
	}
}

// readUntilAny reads a byte slice until one of the char delimiters, up to max bytes.
// It returns a slice with a delimiter and reads the delimiter from the connection.
func (c *Conn) readUntilAny(chars string, max int) ([]byte, error) {
	var value []byte
	for {
		b, err := c.peek()
		if err != nil {
			// TODO: handle EOF
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

func (c *Conn) readChatMsg() (Message, error) {
	// <Bob> hello|
	// or
	// Some info|
	var m ChatMessage

	b, err := c.peek()
	if err != nil {
		return nil, err
	}
	if b[0] == '<' {
		c.discard(1) // trim '<'
		name, err := c.readUntilAny(">", maxUserName)
		if err != nil {
			return nil, fmt.Errorf("cannot read username in chat message: %v", err)
		}
		name = name[:len(name)-1] // trim '>'
		if len(name) == 0 {
			return nil, errors.New("empty name in chat message")
		}
		if err = m.Name.UnmarshalNMDC(name); err != nil {
			return nil, err
		}

		b, err = c.peek()
		if err != nil {
			return nil, err
		}
		if len(b) < 1 || b[0] != ' ' {
			return nil, errors.New("cannot parse chat message")
		}
		c.discard(1) // discard ' '
	}

	msg, err := c.readUntilAny("|", maxChatMsg)
	if err != nil {
		return nil, fmt.Errorf("cannot read chat message: %v", err)
	}
	msg = msg[:len(msg)-1] // trim '|'
	// TODO: convert to UTF8
	if err = m.Text.UnmarshalNMDC(msg); err != nil {
		return nil, err
	}
	if Debug {
		data, _ := m.MarshalNMDC()
		log.Println("<-", string(data))
	}
	return &m, nil
}

func (c *Conn) readCommand() (Message, error) {
	// $Name xxx yyy|
	c.discard(1) // trim '$'

	buf, err := c.readUntilAny("|", maxCmd)
	if err != nil {
		return nil, fmt.Errorf("cannot parse command: %v", err)
	}
	if Debug {
		log.Println("<-", "$"+string(buf))
	}
	buf = buf[:len(buf)-1] // trim '|'

	i := bytes.Index(buf, []byte(" "))
	if i < 0 {
		return UnmarshalMessage(string(buf), nil)
	}
	return UnmarshalMessage(string(buf[:i]), buf[i+1:])
}
