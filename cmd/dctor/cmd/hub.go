package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cretz/bine/tor"
	"github.com/direct-connect/go-dcpp/adc"
)

const torHubPort = 1411

func init() {
	cmd := &cobra.Command{
		Use:   "hub adc://localhost:411",
		Short: "register the hub in Tor network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("local hub address must be specified")
			}
			return runHub(Tor, args[0])
		},
	}
	Root.AddCommand(cmd)
}

func runHub(t *tor.Tor, localHub string) error {
	proxy, err := NewHubProxy(localHub)
	if err != nil {
		return err
	}

	fmt.Println("Registering onion service...")

	// Wait at most a few minutes to publish the service
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Create an onion service to listen on any port but show as 80
	onion, err := t.Listen(ctx, &tor.ListenConf{
		RemotePorts: []int{torHubPort},
	})
	if err != nil {
		return fmt.Errorf("failed to create onion service: %v", err)
	}
	defer onion.Close()

	fmt.Printf("Tor address: adc://%v.onion\n", onion.ID)

	// Run a hub service until terminated
	return proxy.Serve(onion)
}

func NewHubProxy(addr string) (*HubProxy, error) {
	if addr == "" {
		return nil, errors.New("hub address should be set")
	}
	if !strings.HasPrefix(addr, adc.SchemaADC+"://") {
		return nil, errors.New("only adc is currently supported")
	}
	addr = strings.TrimPrefix(addr, adc.SchemaADC+"://")
	return &HubProxy{addr: addr}, nil
}

type HubProxy struct {
	addr string
}

func (p *HubProxy) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := p.ServeConn(conn); err != nil {
				log.Println(err)
			}
		}()
	}
}

func (p *HubProxy) ServeConn(conn net.Conn) error {
	ac, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer ac.Close()

	// connect to the real hub
	hconn, err := net.Dial("tcp", p.addr)
	if err != nil {
		return err
	}
	defer hconn.Close()

	ah, err := adc.NewConn(hconn)
	if err != nil {
		return err
	}
	defer ah.Close()

	c := &hubProxyConn{
		c: ac, hc: ah,
	}

	errc := make(chan error, 2)
	go func() {
		errc <- c.hubLoop()
	}()
	go func() {
		errc <- c.clientLoop()
	}()
	return <-errc
}

type hubProxyConn struct {
	c  *adc.Conn // client side of the connection (tor)
	hc *adc.Conn // hub side of the connection (local)
}

func (c *hubProxyConn) hubLoop() error {
	// FIXME: rewrite all IPs
	return proxyTo(c.c, c.hc)
}

func (c *hubProxyConn) clientLoop() error {
	// FIXME: rewrite all IPs
	return proxyTo(c.hc, c.c)
}
