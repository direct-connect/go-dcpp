package nmdc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEscaping(t *testing.T) {
	var cases = []struct {
		text string
		exp  string
	}{
		{
			text: "text $&|",
			exp:  "text &#36;&amp;&#124;",
		},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			got := Escape(c.text)
			require.Equal(t, c.exp, got)

			got = Unescape(c.exp)
			require.Equal(t, c.text, got)
		})
	}
}
