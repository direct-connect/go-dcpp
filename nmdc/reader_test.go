package nmdc

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var casesReader = []struct {
	name  string
	input string
	exp   []Message
	err   error
}{
	{
		name:  "eof",
		input: ``,
		exp:   nil,
	},
	{
		name:  "pings",
		input: `|||`,
		exp:   nil,
	},
	{
		name:  "empty cmd",
		input: `$|`,
		exp:   nil,
		err: &ErrProtocolViolation{
			Err: errors.New("command name is empty"),
		},
	},
	{
		name:  "GetNickList",
		input: `$GetNickList|`,
		exp: []Message{
			&GetNickList{},
		},
	},
	{
		name:  "null char in command",
		input: "$SomeCommand\x00|",
		err: &ErrProtocolViolation{
			Err: errors.New("command should not contain null characters"),
		},
	},
	{
		name:  "non-ascii in command",
		input: "$Some\tCommand|",
		err: &ErrProtocolViolation{
			Err: errors.New(`command name should be in acsii: "Some\tCommand"`),
		},
	},
	{
		name:  "to",
		input: "$To: alice From: bob $<bob> hi|",
		exp: []Message{
			&PrivateMessage{
				To:   "alice",
				From: "bob",
				Name: "bob",
				Text: "hi",
			},
		},
	},
	{
		name:  "chat",
		input: "<bob> text|",
		exp: []Message{
			&ChatMessage{
				Name: "bob",
				Text: "text",
			},
		},
	},
	{
		name:  "empty chat and trailing",
		input: "<bob>|text" + strings.Repeat(" ", maxName) + "> |",
		exp: []Message{
			&ChatMessage{
				Name: "bob",
			},
			&ChatMessage{
				Text: "text" + strings.Repeat(" ", maxName) + "> ",
			},
		},
	},
}

func TestReader(t *testing.T) {
	for _, c := range casesReader {
		t.Run(c.name, func(t *testing.T) {
			r := NewReader(strings.NewReader(c.input))
			var (
				got  []Message
				gerr error
			)
			for {
				m, err := r.ReadMsg()
				if err == io.EOF {
					break
				} else if err != nil {
					gerr = err
					break
				}
				got = append(got, m)
			}
			if c.err == nil {
				assert.NoError(t, gerr)
			} else {
				assert.Equal(t, c.err, gerr)
			}
			require.Equal(t, c.exp, got)
		})
	}
}
