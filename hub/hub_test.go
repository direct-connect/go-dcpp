package hub

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"testing"
	"time"

	"github.com/direct-connect/go-dcpp/nmdc"
	"github.com/direct-connect/go-dcpp/nmdc/client"

	"github.com/stretchr/testify/require"
)

func init() {
	go http.ListenAndServe(":6060", nil)
}

func newPipe(i int) (net.Conn, net.Conn) {
	hc, cc := net.Pipe()
	localhost := net.ParseIP("127.0.0.1")
	ha := &net.TCPAddr{
		IP:   localhost,
		Port: 411,
	}
	ca := &net.TCPAddr{
		IP:   localhost,
		Port: 1000 + i,
	}
	return addrOverride{
			Conn:   hc,
			local:  ha,
			remote: ca,
		}, addrOverride{
			Conn:   cc,
			local:  ca,
			remote: ha,
		}
}

type addrOverride struct {
	net.Conn
	local, remote net.Addr
}

func (c addrOverride) LocalAddr() net.Addr {
	return c.local
}

func (c addrOverride) RemoteAddr() net.Addr {
	return c.remote
}

func TestHubEnterNMDC(t *testing.T) {
	h, err := NewHub(Config{})
	require.NoError(t, err)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		last error
	)
	setError := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		last = err
	}
	const delay = time.Second
	start := time.Now()
	const count = 5000
	for i := 0; i < count; i++ {
		i := i
		wg.Add(2)
		hc, cc := newPipe(i)
		go func() {
			defer wg.Done()
			defer hc.Close()

			err := h.ServeNMDC(hc, nil)
			if err != nil && err != io.ErrClosedPipe {
				err = fmt.Errorf("hub(%d): %v", i, err)
				log.Println(err)
				setError(err)
				return
			}
		}()
		go func() {
			defer wg.Done()
			defer cc.Close()

			c, err := nmdc.NewConn(cc)
			if err != nil {
				setError(err)
				return
			}
			defer c.Close()

			pc, err := client.HubHandshake(c, &client.Config{
				Name: fmt.Sprintf("peer_%d", i),
			})
			if err != nil {
				err = fmt.Errorf("client(%d): %v", i, err)
				log.Println(err)
				setError(err)
				return
			}
			defer pc.Close()

			err = pc.SendChatMsg(fmt.Sprintf("msg %d", i))
			if err != nil {
				err = fmt.Errorf("client(%d): %v", i, err)
				log.Println(err)
				setError(err)
				return
			}
			time.Sleep(delay)
		}()
	}
	wg.Wait()
	t.Logf("enter in %v", time.Since(start)-delay)
	require.NoError(t, last)
}
