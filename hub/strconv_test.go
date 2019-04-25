package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnquoteSplit(t *testing.T) {
	var cases = []struct {
		name        string
		text        string
		left, right string
	}{
		{
			name: "empty",
			text: `""`,
		},
		{
			name:  "empty space",
			text:  `"" `,
			right: ` `,
		},
		{
			name:  "empty and quote",
			text:  `"" "`,
			right: ` "`,
		},
		{
			name: "escaped",
			text: `"\" "`,
			left: `" `,
		},
		{
			name:  "escaped text",
			text:  `"a\"b "c`,
			left:  `a"b `,
			right: `c`,
		},
		{
			name:  "escaped slash",
			text:  `"a\\"b "c`,
			left:  `a\`,
			right: `b "c`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			left, right, err := cmdUnquoteSplit(c.text)
			require.NoError(t, err)
			require.Equal(t, c.left, left)
			require.Equal(t, c.right, right)
		})
	}
}
