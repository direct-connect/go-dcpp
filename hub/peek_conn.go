package hub

import (
	"io"
	"net"
	"time"
)

func peekCoon(conn net.Conn, peek int) (net.Conn, []byte, error) {
	buf := make([]byte, peek)

	deadline := time.Now().Add(time.Second / 2)
	conn.SetReadDeadline(deadline)

	n, err := conn.Read(buf)
	conn.SetReadDeadline(time.Time{})

	buf = buf[:n]
	if err == io.EOF {
		return conn, buf, io.EOF
	} else if err != nil {
		return conn, buf, err
	}
	return &peekedConn{buf: buf, Conn: conn}, buf, nil
}

type peekedConn struct {
	buf []byte
	net.Conn
}

func (c *peekedConn) Read(p []byte) (int, error) {
	if len(c.buf) != 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}
