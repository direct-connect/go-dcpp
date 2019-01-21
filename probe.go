package dc

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"strings"
	"time"

	"github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/nmdc"
)

var (
	ErrUnsupportedProtocol = errors.New("unsupported protocol")
)

type timeoutErr interface {
	Timeout() bool
}

const (
	nmdcSchema  = nmdc.SchemaNMDC
	nmdcsSchema = "nmdcs://" // TODO: is there a schema for this one?
	adcSchema   = adc.SchemaADC
	adcsSchema  = adc.SchemaADCS
)

func dialContext(ctx context.Context, addr string) (net.Conn, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return net.Dial("tcp", addr)
	}
	timeout := deadline.Sub(time.Now())
	return net.DialTimeout("tcp", addr, timeout)
}

// Probe tries to detect the protocol on a specified host or host:port.
// It returns a canonical address with an appropriate URI scheme.
func Probe(ctx context.Context, addr string) (string, error) {
	if _, port, _ := net.SplitHostPort(addr); port == "" {
		addr += ":411" // TODO: should also try 412, 413, etc
	}

	c, err := dialContext(ctx, addr)
	if err != nil {
		return "", err
	}
	defer c.Close()

	if strings.Contains(addr, "://") {
		// only probe for open port
		return addr, nil
	}

	now := time.Now()
	dt := time.Second * 2

	if deadline, ok := ctx.Deadline(); ok {
		sub := deadline.Sub(now)
		if sub < 0 {
			return "", context.DeadlineExceeded
		}
		if dt > sub/3 {
			dt = sub / 3
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
			return nmdcSchema + addr, nil
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

	// pretend that we speak a base version of ADC
	const adcHandshake = "HSUP ADBAS0 ADBASE ADTIGR\x0a"
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
	if err == nil {
		if string(buf[:n]) == "ISUP " {
			return adcSchema + addr, nil
		}
		return "", ErrUnsupportedProtocol
	}

	// this may sill be ADCS (ADC over TLS), but we broke the connection already
	_ = c.Close()

	// connect again, use (insecure) TLS this time
	c, err = dialContext(ctx, addr)
	if err != nil {
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
		return adcsSchema + addr, nil
	case "nmdc":
		return nmdcsSchema + addr, nil
	}

	now = time.Now()
	curDeadline = now.Add(dt)

	// repeat ADC handshake over TLS this time
	if err = tc.SetWriteDeadline(curDeadline); err != nil {
		return "", err
	}
	_, err = tc.Write([]byte(adcHandshake))
	if err != nil {
		return "", err
	}
	if err = tc.SetReadDeadline(curDeadline); err != nil {
		return "", err
	}
	buf = buf[:5]
	n, err = tc.Read(buf)
	if err == nil && string(buf[:n]) == "ISUP " {
		return adcsSchema + addr, nil
	}
	log.Println(err, string(buf))
	return "", ErrUnsupportedProtocol
}
