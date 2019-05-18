package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/cretz/bine/tor"
	adcp "github.com/direct-connect/go-dc/adc"
	"github.com/direct-connect/go-dcpp/adc"
)

const torClientPort = 1412

func init() {
	cmd := &cobra.Command{
		Use:   "client adc://tor-hub.onion",
		Short: "register the hub in Tor network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("tor hub address must be specified")
			}
			host, _ := cmd.Flags().GetString("host")
			if host == "" {
				return errors.New("local address for client proxy should be specified")
			}
			return runClient(Tor, args[0], host)
		},
	}
	cmd.Flags().String("host", "localhost:1412", "local address for client tor proxy")
	Root.AddCommand(cmd)
}

func runClient(t *tor.Tor, torHub, host string) error {
	fmt.Printf("Forwarding connections to Tor hub: %q\n", torHub)

	// Wait at most a few minutes to connect
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dialer, err := t.Dialer(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to create onion layer: %v", err)
	}

	onion, err := t.Listen(ctx, &tor.ListenConf{
		RemotePorts: []int{torClientPort},
	})
	if err != nil {
		return fmt.Errorf("failed to create onion service: %v", err)
	}
	defer onion.Close()

	fmt.Printf("Tor client address: adc://%v.onion\n", onion.ID)

	proxy, err := NewClientProxy(onion.ID, dialer, torHub)
	if err != nil {
		return err
	}

	fmt.Printf("Listening for clients on %v\n", host)

	// Run a local "hub" proxy service
	l, err := net.Listen("tcp", host)
	if err != nil {
		return err
	}
	defer l.Close()

	go proxy.ServeTor(onion)

	return proxy.Serve(l)
}

func NewClientProxy(onion string, d *tor.Dialer, torHub string) (*ClientProxy, error) {
	if torHub == "" {
		return nil, errors.New("onion hub address should be set")
	}
	if !strings.HasPrefix(torHub, adcp.SchemaADC+"://") {
		return nil, errors.New("only adc is currently supported")
	}
	if !strings.HasSuffix(torHub, torSuffix) {
		return nil, errors.New("only .onion addresses are supported")
	}
	torHub = strings.TrimPrefix(torHub, adcp.SchemaADC+"://")
	torHub = strings.TrimSuffix(torHub, torSuffix)
	return &ClientProxy{onion: onion, dialer: d, torHub: torHub}, nil
}

type torAddr struct {
	onion     string
	localPort int
}

type ctmKey struct {
	cid   adc.CID
	token string
}

type torPeer struct {
	cid   adc.CID
	onion string
}

type ClientProxy struct {
	dialer *tor.Dialer
	torHub string
	onion  string

	mu   sync.RWMutex
	ctms map[ctmKey]torAddr
}

func (p *ClientProxy) dialOnionRaw(onion string, port int) (net.Conn, error) {
	return p.dialer.Dial("tcp", onion+torSuffix+":"+strconv.Itoa(port))
}

func (p *ClientProxy) dialOnion(onion string, port int) (*adc.Conn, error) {
	c, err := p.dialOnionRaw(onion, port)
	if err != nil {
		return nil, err
	}
	ac, err := adc.NewConn(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	return ac, nil
}

func (p *ClientProxy) dial() (*adc.Conn, error) {
	return p.dialOnion(p.torHub, torHubPort)
}

func (p *ClientProxy) Serve(l net.Listener) error {
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

func (p *ClientProxy) ServeTor(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			if err := p.ServeTorConn(conn); err != nil {
				log.Println(err)
			}
		}()
	}
}

func (p *ClientProxy) ServeConn(conn net.Conn) error {
	ac, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer ac.Close()

	ah, err := p.dial()
	if err != nil {
		return err
	}
	defer ah.Close()

	c := &clientProxyConn{
		p: p, c: ac, hc: ah,
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

func (p *ClientProxy) ServeTorConn(conn net.Conn) error {
	buf := make([]byte, len(adc.CID{})+1)
	_, err := io.ReadFull(conn, buf[:])
	if err != nil {
		return err
	}
	var cid adc.CID
	copy(cid[:], buf[:])

	n := buf[len(buf)-1]
	buf = make([]byte, n)
	_, err = io.ReadFull(conn, buf[:])
	if err != nil {
		return err
	}
	token := string(buf)

	log.Printf("tor connection from: %v (token: %s)", cid, token)

	p.mu.RLock()
	ta, ok := p.ctms[ctmKey{cid: cid, token: token}]
	p.mu.RUnlock()
	if !ok {
		return errors.New("invalid token or cid")
	}

	lconn, err := net.Dial("tcp", "localhost:"+strconv.Itoa(ta.localPort))
	if err != nil {
		return err
	}
	defer lconn.Close()

	return copyAll(conn, lconn)
}

type clientProxyConn struct {
	p  *ClientProxy
	c  *adc.Conn // client side of the connection (local)
	hc *adc.Conn // hub side of the connection (tor)

	mu     sync.RWMutex
	cid    adc.CID
	onions map[adc.SID]torPeer
}

func (c *clientProxyConn) hubLoop() error {
	for {
		p, err := c.hc.ReadPacketRaw(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		if cp, ok := p.(adcp.PeerPacket); ok {
			msg := p.Message()
			switch msg.Cmd() {
			case (adcp.Disconnect{}).Cmd():
				c.mu.Lock()
				delete(c.onions, cp.Source())
				c.mu.Unlock()
			case (adcp.UserInfo{}).Cmd():
				var m adcp.UserInfoMod
				if err := cp.DecodeMessageTo(&m); err != nil {
					return err
				}
				mp := make(map[[2]byte]string, len(m))
				for _, f := range m {
					mp[f.Tag] = f.Value
				}
				onion, ok := mp[[2]byte{'E', 'A'}]
				if ok && strings.HasSuffix(onion, torSuffix) {
					if id, ok := mp[[2]byte{'I', 'D'}]; ok {
						log.Printf("user in: %s %s", id, onion)
					}
					c.mu.Lock()
					if c.onions == nil {
						c.onions = make(map[adc.SID]torPeer)
					}
					// TODO: it can be an update, remember?
					scid := mp[[2]byte{'I', 'D'}]
					var cid adc.CID
					if err := cid.UnmarshalADC([]byte(scid)); err != nil {
						return err
					}
					c.onions[cp.Source()] = torPeer{
						cid: cid, onion: strings.TrimSuffix(onion, torSuffix),
					}
					c.mu.Unlock()
				}
			case (adcp.ConnectRequest{}).Cmd():
				var m adcp.ConnectRequest
				if err := cp.DecodeMessageTo(&m); err != nil {
					return err
				}
				l, err := net.Listen("tcp", "localhost:0")
				if err != nil {
					return err
				}
				c.mu.RLock()
				tp, ok := c.onions[cp.Source()]
				c.mu.RUnlock()
				if !ok {
					return errors.New("no onion address")
				}
				rport := m.Port
				m.Port = l.Addr().(*net.TCPAddr).Port
				log.Println("remap:", tp.onion, rport, "<-", "localhost", m.Port)
				p.SetMessage(m)
				go func() {
					if err := c.serveLocalCC(l, tp.onion, m.Token); err != nil {
						log.Println("serveLocalCC:", err)
					}
				}()
			}
		}

		err = c.c.WritePacket(p)
		if err != nil {
			return err
		}
		err = c.c.Flush()
		if err != nil {
			return err
		}
	}
}

func (c *clientProxyConn) clientLoop() error {
	for {
		p, err := c.c.ReadPacketRaw(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		msg := p.Message()
		switch msg.Cmd() {
		case (adcp.UserInfo{}).Cmd():
			var m adcp.UserInfoMod
			if err := p.DecodeMessageTo(&m); err != nil {
				return err
			}
			mp := make(map[[2]byte]string, len(m))
			for _, f := range m {
				mp[f.Tag] = f.Value
			}
			if scid, ok := mp[[2]byte{'I', 'D'}]; ok {
				var cid adc.CID
				err := cid.UnmarshalADC([]byte(scid))
				if err != nil {
					return err
				}
				c.mu.Lock()
				c.cid = cid
				c.mu.Unlock()
			}
			delete(mp, [2]byte{'I', '4'})
			delete(mp, [2]byte{'I', '6'})
			delete(mp, [2]byte{'U', '4'})
			delete(mp, [2]byte{'U', '6'})
			if _, ok := mp[[2]byte{'I', 'D'}]; ok {
				mp[[2]byte{'I', '4'}] = "0.0.0.0"
				mp[[2]byte{'E', 'A'}] = c.p.onion + torSuffix
			}
			m = make(adcp.UserInfoMod, 0, len(mp))
			for k, v := range mp {
				m = append(m, adcp.Field{Tag: k, Value: v})
			}

			log.Println("user out:", m)
			// TODO: features
			p.SetMessage(m)
		case (adcp.ConnectRequest{}).Cmd():
			var m adcp.ConnectRequest
			if err := p.DecodeMessageTo(&m); err != nil {
				return err
			}
			if tp, ok := p.(adcp.TargetPacket); ok {
				c.mu.RLock()
				tp, ok := c.onions[tp.Target()]
				c.mu.RUnlock()
				if !ok {
					return errors.New("no onion address")
				}

				log.Println("remap:", tp.cid, m.Token, tp.onion, "->", "localhost", m.Port)

				c.p.mu.Lock()
				if c.p.ctms == nil {
					c.p.ctms = make(map[ctmKey]torAddr)
				}
				c.p.ctms[ctmKey{cid: tp.cid, token: m.Token}] = torAddr{onion: tp.onion, localPort: m.Port}
				c.p.mu.Unlock()
			}
		}

		err = c.hc.WritePacket(p)
		if err != nil {
			return err
		}
		err = c.hc.Flush()
		if err != nil {
			return err
		}
	}
}

func (c *clientProxyConn) serveLocalCC(l net.Listener, onion string, token string) error {
	defer l.Close()

	c1, err := l.Accept()
	if err != nil {
		return err
	}
	defer c1.Close()

	log.Println("got local connection for", onion)

	c2, err := c.p.dialOnionRaw(onion, torClientPort)
	if err != nil {
		return err
	}
	defer c2.Close()

	c.mu.RLock()
	cid := c.cid
	c.mu.RUnlock()

	log.Println("local cid:", cid)

	buf := make([]byte, len(cid)+1+len(token))
	i := copy(buf, cid[:])
	buf[i] = byte(len(token))
	i++
	copy(buf[i:], token)

	_, err = c2.Write(buf[:])
	if err != nil {
		return err
	}

	return copyAll(c1, c2)
}
