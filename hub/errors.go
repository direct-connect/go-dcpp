package hub

import (
	"errors"
	"strconv"
	"strings"
	"syscall"

	"github.com/direct-connect/go-dc/nmdc"
)

var (
	errNickTaken       = errors.New("nick taken")
	errConnInsecure    = errors.New("connection is insecure")
	errCmdInvalidArg   = errors.New("invalid argument")
	errServerIsPrivate = errors.New("server is private")
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

func isTooManyFDs(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EMFILE || errno == syscall.ENFILE
	}
	return strings.Contains(err.Error(), "too many open files")
}

func isProtocolErr(err error) bool {
	var e1 *ErrUnknownProtocol
	var e2 *nmdc.ErrProtocolViolation
	var e3 *nmdc.ErrUnexpectedCommand
	return errors.As(err, &e1) || errors.As(err, &e2) || errors.As(err, &e3)
}
