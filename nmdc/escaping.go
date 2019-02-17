package nmdc

import (
	"html"
	"strings"
)

var legacyUnescaper = strings.NewReplacer(
	"/%DCN000%/", "\x00",
	"/%DCN005%/", "\x05",
	"/%DCN036%/", "$",
	"/%DCN096%/", "`",
	"/%DCN124%/", "|",
	"/%DCN126%/", "~",
)

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"$", "&#36;",
	"|", "&#124;",
)

var htmlEscaperName = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"$", "&#36;",
	"|", "&#124;",
)

func Escape(s string) string {
	return htmlEscaper.Replace(s)
}

func EscapeName(s string) string {
	return htmlEscaperName.Replace(s)
}

func Unescape(s string) string {
	s = legacyUnescaper.Replace(s)
	s = html.UnescapeString(s)
	return s
}
