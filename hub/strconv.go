package hub

import (
	"strconv"
	"unicode/utf8"
)

func cmdUnquoteSplit(s string) (string, string, error) {
	n := len(s)
	if n < 2 {
		return "", "", strconv.ErrSyntax
	}
	const quote = '"'
	if s[0] != quote {
		return "", "", strconv.ErrSyntax
	}
	s = s[1:]

	var (
		buf     []byte
		runeTmp [utf8.UTFMax]byte
	)
	for {
		if len(s) == 0 {
			return "", "", strconv.ErrSyntax
		} else if s[0] == quote {
			s = s[1:]
			break
		}
		c, multibyte, ss, err := strconv.UnquoteChar(s, quote)
		if err != nil {
			return "", s, err
		}
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	return string(buf), s, nil
}
