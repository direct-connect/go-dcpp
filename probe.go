package dc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"time"

	nmdcp "github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/adc"
)

var (
	ErrUnsupportedProtocol = errors.New("unsupported protocol")
)

type timeoutErr interface {
	Timeout() bool
}

const (
	probeTimeout = time.Second * 5

	nmdcSchema  = nmdcp.SchemeNMDC
	nmdcsSchema = nmdcp.SchemeNMDCS
	adcSchema   = adc.SchemaADC
	adcsSchema  = adc.SchemaADCS
)

func dialContext(ctx context.Context, addr string) (net.Conn, error) {
	timeout := probeTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = deadline.Sub(time.Now())
	}
	return net.DialTimeout("tcp", addr, timeout)
}

// Probe tries to detect the protocol on a specified host or host:port.
// It returns a canonical address with an appropriate URI scheme.
func Probe(rctx context.Context, addr string) (*url.URL, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		// may be a hostname:port
		if _, _, err := net.SplitHostPort(addr); err != nil {
			// assume it's a hostname only
			addr += ":411" // TODO: should also try 412, 413, etc
		}
		u = &url.URL{Host: addr}
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := context.WithCancel(rctx)
	defer cancel()

	// race TLS and plaintext connection
	errc := make(chan error, 2)
	outPlain := make(chan string, 1)
	outTLS := make(chan string, 1)

	wg.Add(2)
	go func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				errc <- fmt.Errorf("panic: %v", r)
			}
		}()
		proto, err := probeTLS(ctx, u.Host)
		if err != nil {
			errc <- err
			return
		}
		outTLS <- proto
	}()
	go func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				errc <- fmt.Errorf("panic: %v", r)
			}
		}()
		proto, err := probePlain(ctx, u.Host)
		if err != nil {
			errc <- err
			return
		}
		outPlain <- proto
	}()

	var (
		protoPlain string
		err1       error
	)
	select {
	case protoTLS := <-outTLS:
		// prefer TLS
		u.Scheme = protoTLS
		return u, nil
	case protoPlain = <-outPlain:
		// wait for TLS response
	case err1 = <-errc:
		// wait for the second one
	}

	select {
	case protoTLS := <-outTLS:
		// prefer TLS
		u.Scheme = protoTLS
		return u, nil
	case protoPlain = <-outPlain:
		// the first error was from TLS
		u.Scheme = protoPlain
		return u, nil
	case err := <-errc:
		if err1 == nil {
			// TLS failed, but plaintext was successful
			u.Scheme = protoPlain
			return u, nil
		}
		// both attempts failed
		if err == ErrUnsupportedProtocol && err1 == ErrUnsupportedProtocol {
			return u, err
		} else if e, ok := err.(timeoutErr); ok && e.Timeout() {
			// return any if it's a timeout
			return u, err
		}
		// return both
		return u, fmt.Errorf("probe failed: %v; %v", err1, err)
	}
}

// pretend that we speak a base version of ADC
const adcHandshake = "HSUP ADBAS0 ADBASE ADTIGR\x0a"

func probeTLS(ctx context.Context, addr string) (string, error) {
	c, err := dialContext(ctx, addr)
	if err != nil {
		return "", err
	}
	defer c.Close()

	now := time.Now()
	dt := probeTimeout
	if deadline, ok := ctx.Deadline(); ok {
		sub := deadline.Sub(now)
		if sub < 0 {
			return "", context.DeadlineExceeded
		}
		if dt > sub {
			dt = sub
		}
	}
	if err = c.SetDeadline(now.Add(dt)); err != nil {
		return "", err
	}

	tc := tls.Client(c, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"adc", "nmdc"},
	})
	defer tc.Close()

	if err = tc.Handshake(); err != nil {
		return "", ErrUnsupportedProtocol
	}

	// first, check if ALPN handshake was successful
	state := tc.ConnectionState()
	switch state.NegotiatedProtocol {
	case "adc":
		return adcsSchema, nil
	case "nmdc":
		return nmdcsSchema, nil
	}

	// repeat ADC handshake over TLS this time
	if err = tc.SetDeadline(now.Add(dt)); err != nil {
		return "", err
	}
	_, err = tc.Write([]byte(adcHandshake))
	if err != nil {
		return "", err
	}
	buf := make([]byte, 5)
	n, err := tc.Read(buf)
	if err == nil && string(buf[:n]) == "ISUP " {
		return adcsSchema, nil
	}
	return "", ErrUnsupportedProtocol
}

func probePlain(ctx context.Context, addr string) (string, error) {
	c, err := dialContext(ctx, addr)
	if err != nil {
		return "", err
	}
	defer c.Close()

	now := time.Now()
	dt := probeTimeout

	if deadline, ok := ctx.Deadline(); ok {
		sub := deadline.Sub(now)
		if sub < 0 {
			return "", context.DeadlineExceeded
		}
		if dt > sub {
			dt = sub
		}
	}

	if err = c.SetReadDeadline(now.Add(dt)); err != nil {
		return "", err
	}

	buf := make([]byte, 6)
	n, err := c.Read(buf)
	if err == nil {
		// may be NMDC protocol where server speaks first
		if string(buf[:n]) == "$Lock " {
			return nmdcSchema, nil
		}
		return "", ErrUnsupportedProtocol
	}
	te, ok := err.(timeoutErr)
	if !ok || !te.Timeout() {
		return "", err
	}
	// timeout, server expects that we speak first

	now = time.Now()
	curDeadline := now.Add(dt)
	if err = c.SetWriteDeadline(curDeadline); err != nil {
		return "", err
	}
	_, err = c.Write([]byte(adcHandshake))
	if err != nil {
		// FIXME: server may drop the connection earlier, since we waited too long
		return "", err
	}
	if err = c.SetReadDeadline(curDeadline); err != nil {
		return "", err
	}
	buf = buf[:5]
	n, err = c.Read(buf)
	if err == io.EOF {
		return "", ErrUnsupportedProtocol
	} else if err != nil {
		return "", err
	}
	if string(buf[:n]) == "ISUP " {
		return adcSchema, nil
	}
	return "", ErrUnsupportedProtocol
}
