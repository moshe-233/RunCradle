package output

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var terminalCodes = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")

func Clean(s string, bytes int) string {
	s = terminalCodes.ReplaceAllString(strings.ToValidUTF8(s, "�"), "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > bytes {
		s = s[:bytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
