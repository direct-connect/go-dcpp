package nmdc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"

	"golang.org/x/text/encoding"
)

const (
	readBuf    = 4096
	maxName    = 256
	maxCmdName = 32
	maxLine    = readBuf * 8
)

var (
	errValueIsTooLong  = errors.New("value is too long")
	errExpectedCommand = errors.New("nmdc: expected command, got chat message")
	errExpectedChat    = errors.New("nmdc: chat message, got command")
)

type ErrUnexpectedCommand struct {
	Expected string
	Received *RawCommand
}

func (e *ErrUnexpectedCommand) Error() string {
	return fmt.Sprintf("nmdc: expected %q, got %q", e.Expected, e.Received.Name)
}

type ErrProtocolViolation struct {
	Err error
}

func (e *ErrProtocolViolation) Error() string {
	return fmt.Sprintf("nmdc: protocol error: %v", e.Err)
}

type errUnknownEncoding struct {
	text []byte
}

func (e *errUnknownEncoding) Error() string {
	return fmt.Sprintf("nmdc: unknown text encoding: %q", string(e.text))
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r: r, buf: make([]byte, 0, readBuf),
		maxLine:    maxLine,
		maxCmdName: maxCmdName,
	}
}

type Reader struct {
	r   io.Reader
	err error

	maxLine    int
	maxCmdName int

	buf  []byte
	i    int
	mbuf bytes.Buffer

	// dec is the current decoder for the text values.
	// It converts connection encoding to UTF8. Nil value means that connection uses UTF8.
	dec *encoding.Decoder

	// OnLine is called each time a raw protocol line is read from the connection.
	// The buffer will contain a '|' delimiter and is in the connection encoding.
	// The function may return (false, nil) to ignore the message.
	OnLine func(line []byte) (bool, error)

	// OnKeepAlive is called when an empty (keep-alive) message is received.
	OnKeepAlive func() error

	// OnRawCommand is called each time a message is received.
	// Protocol commands will have a non-nil name, while chat messages will have a nil name.
	// The function may return (false, nil) to ignore the message.
	OnRawMessage func(cmd, args []byte) (bool, error)

	// OnUnknownEncoding is called when a text with non-UTF8 encoding is received.
	// It may either return a new decoder or return an error to fail the decoding.
	OnUnknownEncoding func(text []byte) (*encoding.Decoder, error)

	// OnMessage is called each time a protocol message is decoded.
	// The function may return (false, nil) to ignore the message.
	OnMessage func(m Message) (bool, error)
}

// SetMaxLine sets a maximal length of the protocol messages in bytes, including the delimiter.
func (r *Reader) SetMaxLine(n int) {
	r.maxLine = n
}

// SetMaxCmdName sets a maximal length of the protocol command name in bytes.
func (r *Reader) SetMaxCmdName(n int) {
	r.maxCmdName = n
}

// SetDecoder sets a text decoder for the connection.
func (r *Reader) SetDecoder(dec *encoding.Decoder) {
	r.dec = dec
}

func (r *Reader) peek() ([]byte, error) {
	if r.i < len(r.buf) {
		return r.buf[r.i:], nil
	}
	r.i = 0
	r.buf = r.buf[:cap(r.buf)]
	n, err := r.r.Read(r.buf)
	r.buf = r.buf[:n]
	return r.buf, err
}

func (r *Reader) discard(n int) {
	if n < 0 {
		r.i += len(r.buf)
	} else {
		r.i += n
	}
}

// readUntil reads a byte slice until the delimiter, up to max bytes.
// It returns a newly allocated slice with a delimiter and reads bytes and the delimiter
// from the reader.
func (r *Reader) readUntil(delim string, max int) ([]byte, error) {
	r.mbuf.Reset()
	for {
		b, err := r.peek()
		if err != nil {
			return nil, err
		}
		i := bytes.Index(b, []byte(delim))
		if i >= 0 {
			r.mbuf.Write(b[:i+len(delim)])
			r.discard(i + len(delim))
			return r.mbuf.Bytes(), nil
		}
		if r.mbuf.Len()+len(b) > max {
			return nil, errValueIsTooLong
		}
		r.mbuf.Write(b)
		r.discard(-1)
	}
}

// ReadLine reads a single raw message until the delimiter '|'. The returned buffer
// contains a '|' delimiter and is in the connection encoding. The buffer is only valid
// until the next call to Read or ReadLine.
func (r *Reader) ReadLine() ([]byte, error) {
	if err := r.err; err != nil {
		return nil, err
	}
	for {
		data, err := r.readUntil("|", r.maxLine)
		if err == errValueIsTooLong {
			r.err = &ErrProtocolViolation{
				Err: fmt.Errorf("cannot read message: %v", err),
			}
			return nil, r.err
		} else if err != nil {
			r.err = err
			return nil, err
		}
		if r.OnLine != nil {
			if ok, err := r.OnLine(data); err != nil {
				r.err = err
				return nil, err
			} else if !ok {
				continue // drop
			}
		}
		if bytes.ContainsAny(data, "\x00") {
			r.err = &ErrProtocolViolation{
				Err: errors.New("message should not contain null characters"),
			}
			return nil, r.err
		}
		return data, nil
	}
}

// ReadMsg reads a single message.
func (r *Reader) ReadMsg() (Message, error) {
	var m Message
	if err := r.readMsgTo(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// ReadMsgTo will read a message to a pointer passed to the function.
// If the message read has a different type, an error will be returned.
func (r *Reader) ReadMsgTo(m Message) error {
	if m == nil {
		panic("nil message to decode")
	}
	return r.readMsgTo(&m)
}

func (r *Reader) readMsgTo(ptr *Message) error {
	for {
		line, err := r.ReadLine()
		if err != nil {
			return err
		}
		line = bytes.TrimSuffix(line, []byte("|"))
		if len(line) == 0 {
			// keep-alive
			if r.OnKeepAlive != nil {
				if err := r.OnKeepAlive(); err != nil {
					return err
				}
			}
			continue // keep alive, ignore
		}
		var (
			out  = *ptr
			cmd  []byte
			args []byte
		)
		if line[0] == '$' {
			line = line[1:]
			// protocol command
			cmd, args = line, nil // only name
			if i := bytes.Index(line, []byte(" ")); i >= 0 {
				cmd, args = line[:i], line[i+1:] // name and args
			}
			if r.OnRawMessage != nil {
				if ok, err := r.OnRawMessage(cmd, args); err != nil {
					return err
				} else if !ok {
					continue // drop
				}
			}
			if len(cmd) == 0 {
				return &ErrProtocolViolation{
					Err: errors.New("command name is empty"),
				}
			} else if len(cmd) > r.maxCmdName {
				return &ErrProtocolViolation{
					Err: errors.New("command name is too long"),
				}
			} else if !isASCII(cmd) {
				return &ErrProtocolViolation{
					Err: fmt.Errorf("command name should be in acsii: %q", string(cmd)),
				}
			}
			if out == nil {
				// detect type by command name
				name := string(cmd)
				if rt, ok := messages[name]; ok {
					out = reflect.New(rt).Interface().(Message)
				} else {
					out = &RawCommand{Name: name}
				}
				*ptr = out
			} else if _, ok := out.(*ChatMessage); ok {
				return errExpectedCommand
			} else if got := string(cmd); out.Cmd() != got {
				return &ErrUnexpectedCommand{
					Expected: out.Cmd(),
					Received: &RawCommand{
						Name: got, Data: args,
					},
				}
			}
		} else {
			// chat message
			cmd, args = nil, line
			if r.OnRawMessage != nil {
				if ok, err := r.OnRawMessage(cmd, args); err != nil {
					return err
				} else if !ok {
					continue // drop
				}
			}
			if out == nil {
				out = &ChatMessage{}
				*ptr = out
			} else if _, ok := out.(*ChatMessage); !ok {
				return errExpectedChat
			}
		}
		err = out.UnmarshalNMDC(r.dec, args)
		if r.OnUnknownEncoding != nil {
			if e, ok := err.(*errUnknownEncoding); ok {
				dec, err := r.OnUnknownEncoding(e.text)
				if err != nil {
					return err
				} else if dec == nil {
					return e
				}
				r.dec = dec
				err = out.UnmarshalNMDC(r.dec, args)
			}
		}
		if err != nil {
			return err
		}
		if r.OnMessage != nil {
			if ok, err := r.OnMessage(out); err != nil {
				return err
			} else if !ok {
				continue // drop
			}
		}
		return nil
	}
}

func isASCII(p []byte) bool {
	for _, b := range p {
		if b == '/' || b == '-' || b == '_' || b == '.' || b == ':' {
			continue
		}
		if b < '0' || b > 'z' {
			return false
		}
		if b >= 'a' && b <= 'z' {
			continue
		}
		if b >= 'A' && b <= 'Z' {
			continue
		}
		if b >= '0' && b <= '9' {
			continue
		}
		return false
	}
	return true
}
