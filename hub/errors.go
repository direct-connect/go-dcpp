package hub

import (
	"errors"
	"strconv"
)

var (
	errNickTaken = errors.New("nick taken")
)

type ErrUnknownProtocol struct {
	Magic  []byte
	Secure bool
}

func (e *ErrUnknownProtocol) Error() string {
	tls := ""
	if e.Secure {
		tls = " (TLS)"
	}
	return "unknown protocol magic: " + strconv.Quote(string(e.Magic)) + tls
}
