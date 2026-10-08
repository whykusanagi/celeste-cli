package termsafe

import (
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"plain\ttext\nline two": "plain\ttext\nline two",
		"crlf\r\nline":          "crlf\nline",
		"a\rb":                  `a\rb`,
		"x\x1b]0;t\x07y":        `x\x1b]0;t\ay`,
		"x\x1b[2Ky":             `x\x1b[2Ky`,
		"c1\u009bz":             `c1\u009bz`,
		"del\x7f":               `del\x7f`,
		"bidi\u202eevil":        `bidi\u202eevil`,
		"sep\u2028x":            `sep\u2028x`,
		"bad\xffbyte":           `bad\xffbyte`,
		"emoji 👩‍💻 セレステ":        "emoji 👩‍💻 セレステ",
		"":                      "",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStyledKeepsSGROnly(t *testing.T) {
	in := "\x1b[38;2;255;0;128mpink\x1b[0m \x1b]52;c;Zm9v\x07 \x1b[2J\x1b[1A"
	got := Styled(in)
	if !strings.HasPrefix(got, "\x1b[38;2;255;0;128mpink\x1b[0m ") {
		t.Errorf("Styled dropped celeste's own color: %q", got)
	}
	rest := strings.TrimPrefix(got, "\x1b[38;2;255;0;128mpink\x1b[0m ")
	if strings.ContainsAny(rest, "\x1b\x07") {
		t.Errorf("Styled kept a non-SGR control: %q", got)
	}
}

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"rm -rf build":   "rm -rf build",
		"echo ok\rsafe":  `"echo ok\rsafe"`,
		"two\nlines":     `"two\nlines"`,
		"x\x1b]0;t\x07":  `"x\x1b]0;t\a"`,
		"bidi\u202eevil": `"bidi\u202eevil"`,
		"セレステ":           "セレステ",
	} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzText(f *testing.F) {
	f.Add("a\x1b[31mb\x1b]0;x\x07\r\n\u202e\xff")
	f.Fuzz(func(t *testing.T, s string) {
		for _, out := range []string{Text(s), Line(s)} {
			if strings.ContainsAny(out, "\x1b\r\x07\u202e\u009b") {
				t.Fatalf("control survived: %q -> %q", s, out)
			}
		}
		if out := Styled(s); strings.ContainsAny(out, "\r\x07\u202e\u009b") {
			t.Fatalf("control survived Styled: %q -> %q", s, out)
		}
	})
}
