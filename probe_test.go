package dc

import (
	"context"
	"testing"
)

func TestProbe(t *testing.T) {
	var cases = []struct {
		addr string
		exp  string
	}{
		{
			addr: "dc.ozerki.pro",
			exp:  "dchub://dc.ozerki.pro:411",
		},
		{
			addr: "adc.podryad.tv",
			exp:  "adc://adc.podryad.tv:411",
		},
		{
			addr: "dc-united.ddns.net:9111",
			exp:  "adcs://dc-united.ddns.net:9111",
		},
	}
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			u, err := Probe(context.Background(), c.addr)
			if err != nil {
				t.Fatal(err)
			} else if c.exp != u.String() {
				t.Fatal("unexpected address:", u.String())
			}
		})
	}
}
