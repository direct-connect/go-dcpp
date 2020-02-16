package hub

import (
	"errors"
	"net"
	"os"
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
	switch e := err.(type) {
	case *net.OpError:
		return isTooManyFDs(e.Err)
	case *os.SyscallError:
		return isTooManyFDs(e.Err)
	case syscall.Errno:
		switch e {
		case syscall.EMFILE, syscall.ENFILE:
			return true
		}
	}
	return strings.Contains(err.Error(), "too many open files")
}

func isProtocolErr(err error) bool {
	switch err.(type) {
	case *ErrUnknownProtocol:
		return true
	case *nmdc.ErrProtocolViolation:
		return true
	case *nmdc.ErrUnexpectedCommand:
		return true
	}
	return false
}
