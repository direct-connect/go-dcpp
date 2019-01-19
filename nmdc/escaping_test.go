package nmdc

import "testing"

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
			if got != c.exp {
				t.Fatalf("unexpected escaping: %q", got)
			}
			got = Unescape(c.exp)
			if got != c.text {
				t.Fatalf("unexpected text: %q", got)
			}
		})
	}
}
