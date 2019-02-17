package autoreg

import (
	"io"
	"testing"
)

func TestAutoreg(t *testing.T) {
	info := Info{
		Name:     "Some hub name",
		Host:     "localhost:411",
		Desc:     "Long |hub| description",
		Users:    10,
		Share:    1023,
		MinShare: 4 * mb,
	}
	ch := make(chan Info, 1)
	errc := make(chan error, 1)
	srv := NewServer(RegistryFunc(func(info Info) error {
		ch <- info
		return nil
	}))
	pr1, pw1 := io.Pipe()
	pr2, pw2 := io.Pipe()
	go func() {
		err := srv.serve(struct {
			io.Reader
			io.Writer
			io.Closer
		}{
			pr1, pw2, pw2,
		})
		if err != nil {
			errc <- err
		}
	}()
	err := registerOn(struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		pr2, pw1, pw1,
	}, info, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-errc:
		t.Fatal(err)
	case info2 := <-ch:
		if info != info2 {
			t.Fatalf("\n%#v\nvs\n%#v", info, info2)
		}
	}
}
