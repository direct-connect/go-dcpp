package nmdc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding"
)

const (
	readBuf    = 4096
	maxName    = 256
	maxText    = readBuf * 4
	maxChatMsg = maxText
	maxCmd     = readBuf * 2
	maxCmdName = 32
)

var (
	errValueIsTooLong  = errors.New("value is too long")
	errExpectedCommand = errors.New("nmdc: expected command, got chat message")
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

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r, buf: make([]byte, 0, readBuf)}
}

type Reader struct {
	r   io.Reader
	err error

	buf []byte
	i   int

	// dec is the current decoder for the text values.
	// It converts connection encoding to UTF8. Nil value means that connection uses UTF8.
	dec *encoding.Decoder

	// OnKeepAlive is called when an empty (keep-alive) message is received.
	OnKeepAlive func() error

	// OnUnknownEncoding is called when a text with non-UTF8 encoding is received.
	// The function may either return a different decoder and a new string, or
	// return an error to cancel the decoding.
	OnUnknownEncoding func(text string) (string, *encoding.Decoder, error)

	// OnChatMessage is called each time a chat message is parsed.
	// The function may return false to drop the message, or error to return to the caller.
	OnChatMessage func(m *ChatMessage) (bool, error)

	// OnRawCommand is called each time a protocol command is parsed.
	// The function may return false to drop the command, or error to return to the caller.
	OnRawCommand func(m *RawCommand) (bool, error)

	// OnCommand is called each time a protocol command is decoded.
	// The function may return false to drop the command, or error to return to the caller.
	OnCommand func(m Message) (bool, error)
}

// SetDecoder sets a text decoder for the connection.
func (r *Reader) SetDecoder(dec *encoding.Decoder) {
	r.dec = dec
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

func (r *Reader) readMsgTo(ptr *Message) error {
	if err := r.err; err != nil {
		return err
	}

	for {
		b, err := r.peek()
		if err != nil {
			r.err = err
			return err
		} else if len(b) == 0 {
			continue
		}
		if b[0] == '|' {
			r.discard(1)
			if r.OnKeepAlive != nil {
				if err := r.OnKeepAlive(); err != nil {
					return err
				}
			}
			continue // keep alive, skip
		}
		out := *ptr
		if b[0] != '$' {
			// not a command -> chat message
			// check that we expect a chat message as well
			m, ok := out.(*ChatMessage)
			if !ok {
				if out != nil {
					r.err = errExpectedCommand
					return r.err
				}
				m = &ChatMessage{}
				*ptr = m
			}
			err := r.readChatMsg(m)
			if err != nil {
				r.err = err
				return err
			}
			if r.OnChatMessage != nil {
				// call chat message hook
				ok, err := r.OnChatMessage(m)
				if err != nil {
					return err
				} else if !ok {
					continue // drop
				}
			}
			return nil
		}
		// protocol command
		raw, err := r.readRawCommand()
		if err != nil {
			r.err = err
			return err
		}
		if r.OnRawCommand != nil {
			// call raw protocol command hook
			ok, err := r.OnRawCommand(raw)
			if err != nil {
				return err
			} else if !ok {
				continue // drop
			}
		}
		if out == nil {
			// do not expect a specific message - decode any
			m, err := raw.Decode(r.dec)
			if err != nil {
				return err
			}
			if r.OnCommand != nil {
				// call protocol command hook
				ok, err := r.OnCommand(m)
				if err != nil {
					return err
				} else if !ok {
					continue // drop
				}
			}
			*ptr = m
			return nil
		}
		// expect a specific message - check the type before decoding
		if out.Cmd() != raw.Name {
			// TODO: this case also catches cases when we expect chat messages
			return &ErrUnexpectedCommand{
				Expected: out.Cmd(),
				Received: raw,
			}
		}
		err = out.UnmarshalNMDC(r.dec, raw.Data)
		if err != nil {
			return err
		}
		if r.OnCommand != nil {
			// call protocol command hook
			ok, err := r.OnCommand(out)
			if err != nil {
				return err
			} else if !ok {
				continue // drop
			}
		}
		return nil
	}
}

// readUntil reads a byte slice until the delimiter, up to max bytes.
// It returns a newly allocated slice with a delimiter and reads bytes and the delimiter
// from the reader.
func (r *Reader) readUntil(delim string, max int) ([]byte, error) {
	var value []byte
	for {
		b, err := r.peek()
		if err != nil {
			return nil, err
		}
		i := bytes.Index(b, []byte(delim))
		if i >= 0 {
			value = append(value, b[:i+len(delim)]...)
			r.discard(i + len(delim))
			return value, nil
		}
		if len(value)+len(b) > max {
			return nil, errValueIsTooLong
		}
		value = append(value, b...)
		r.discard(-1)
	}
}

func (r *Reader) readChatMsg(m *ChatMessage) error {
	*m = ChatMessage{}
	// <Bob> hello|
	// or
	// Some info|

	b, err := r.peek()
	if err != nil {
		return err
	}
	if b[0] == '<' {
		r.discard(1) // trim '<'
		name, err := r.readUntil("> ", maxName)
		if err == errValueIsTooLong {
			return &ErrProtocolViolation{
				Err: fmt.Errorf("cannot read username in chat message: %v", err),
			}
		} else if err != nil {
			return err
		}
		name = name[:len(name)-2] // trim '> '
		if len(name) == 0 {
			return &ErrProtocolViolation{
				Err: errors.New("empty name in chat message"),
			}
		}
		if err = m.Name.UnmarshalNMDC(r.dec, name); err != nil {
			return &ErrProtocolViolation{Err: err}
		}
	}

	msg, err := r.readUntil("|", maxChatMsg)
	if err == errValueIsTooLong {
		return &ErrProtocolViolation{
			Err: fmt.Errorf("cannot read chat message: %v", err),
		}
	} else if err != nil {
		return err
	}
	msg = msg[:len(msg)-1] // trim '|'

	if bytes.ContainsAny(msg, "\x00") {
		return &ErrProtocolViolation{
			Err: errors.New("chat message should not contain null characters"),
		}
	}
	var text String
	if err = text.UnmarshalNMDC(r.dec, msg); err != nil {
		return &ErrProtocolViolation{Err: err}
	}
	if r.dec == nil && r.OnUnknownEncoding != nil && !utf8.ValidString(string(text)) {
		str, dec, err := r.OnUnknownEncoding(string(text))
		if err != nil {
			return err
		}
		if dec != nil {
			r.dec = dec
			text = String(str)
		}
	}
	m.Text = string(text)
	return nil
}

func (r *Reader) readRawCommand() (*RawCommand, error) {
	// $Name xxx yyy|
	buf, err := r.readUntil("|", maxCmd)
	if err == errValueIsTooLong {
		return nil, &ErrProtocolViolation{
			Err: fmt.Errorf("cannot parse command: %v", err),
		}
	} else if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(buf, []byte{'$'}) {
		return nil, errors.New("expected command, got chat message")
	}
	buf = buf[1 : len(buf)-1] // trim '$' and '|'

	if bytes.Contains(buf, []byte{0x00}) {
		return nil, &ErrProtocolViolation{
			Err: errors.New("command should not contain null characters"),
		}
	}
	i := bytes.Index(buf, []byte(" "))
	var (
		name []byte
		data []byte
	)
	if i < 0 {
		name = buf
	} else {
		name = buf[:i]
		data = buf[i+1:]
	}
	if len(name) == 0 {
		return nil, &ErrProtocolViolation{
			Err: errors.New("command name is empty"),
		}
	} else if len(name) > maxCmdName {
		return nil, &ErrProtocolViolation{
			Err: errors.New("command name is too long"),
		}
	} else if !isASCII(name) {
		return nil, &ErrProtocolViolation{
			Err: fmt.Errorf("command name should be in acsii: %q", string(name)),
		}
	}
	return &RawCommand{Name: string(name), Data: data}, nil
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
