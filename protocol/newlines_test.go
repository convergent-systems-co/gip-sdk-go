package protocol

import "testing"

func TestNormalizeNewlines(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"crlf", "line one\r\nline two\r\n", "line one\nline two\n"},
		{"lone cr", "line one\rline two\r", "line one\nline two\n"},
		{"mixed", "a\r\nb\rc\nd\r\r\ne", "a\nb\nc\nd\n\ne"},
		{"no-op", "already\nLF only\n", "already\nLF only\n"},
		{"empty", "", ""},
	} {
		if got := NormalizeNewlines(c.in); got != c.want {
			t.Errorf("%s: NormalizeNewlines(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
