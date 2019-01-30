package nmdc

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ net.Conn = (*Discard)(nil)

type Discard struct {
	n  uint32
	dt time.Duration
}

func (d *Discard) Read(b []byte) (int, error) {
	panic("implement me")
}

func (d *Discard) Write(b []byte) (int, error) {
	if d.dt != 0 {
		time.Sleep(d.dt)
	}
	atomic.AddUint32(&d.n, uint32(len(b)))
	return len(b), nil
}

func (Discard) Close() error {
	return nil
}

func (Discard) LocalAddr() net.Addr {
	panic("implement me")
}

func (Discard) RemoteAddr() net.Addr {
	panic("implement me")
}

func (Discard) SetDeadline(t time.Time) error {
	return nil
}

func (Discard) SetReadDeadline(t time.Time) error {
	return nil
}

func (Discard) SetWriteDeadline(t time.Time) error {
	return nil
}

func BenchmarkConnWriteLinear(b *testing.B) {
	d := &Discard{dt: time.Millisecond}
	data := make([]byte, 1024)
	c, err := NewConn(d)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := c.writeRaw(data)
		if err != nil {
			b.Fatal(err)
		}
	}
	err = c.Flush()
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkConnWrite(b *testing.B) {
	d := &Discard{dt: time.Millisecond}
	data := make([]byte, 1024)
	c, err := NewConn(d)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := c.writeOneRaw(data)
		if err != nil {
			b.Fatal(err)
		}
	}
	err = c.Flush()
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkConnWriteConcurrent(b *testing.B) {
	d := &Discard{dt: time.Millisecond}
	data := make([]byte, 1024)
	c, err := NewConn(d)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = c.writeOneRaw(data)
		}()
	}
	b.ResetTimer()
	close(start)
	wg.Wait()
	err = c.Flush()
	if err != nil {
		b.Fatal(err)
	}
}
