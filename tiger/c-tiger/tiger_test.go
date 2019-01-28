package tiger_test

import (
	"bytes"
	"testing"

	tigerc "github.com/direct-connect/go-dcpp/tiger/c-tiger"
	tigerg "github.com/direct-connect/go-dcpp/tiger/go-tiger"
)

func hashG(p []byte) (out tigerc.TH) {
	h := tigerg.New()
	h.Write(p)
	res := h.Sum(out[:0])
	if cap(res) != cap(out[:]) {
		panic("diff sizes")
	}
	return
}

func hashR(p []byte) tigerc.TH {
	return tigerc.Tiger(p)
}

func TestTiger(t *testing.T) {
	t.Skip()
	const max = 1024
	buf := bytes.Repeat([]byte{'a'}, max)
	for i := 1; i < 1024; i++ {
		b := buf[:i]
		h1 := hashG(b)
		h2 := hashR(b)
		if h1 != h2 {
			t.Errorf("size %d: %x vs %x", i, h1, h2)
		}
	}
}
