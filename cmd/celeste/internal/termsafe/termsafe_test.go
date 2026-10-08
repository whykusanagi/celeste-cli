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

// Styled keeps colors but never the SGR that hides text (conceal, 8):
// untrusted text in a system line could otherwise make itself invisible.
func TestStyledDropsConceal(t *testing.T) {
	for _, in := range []string{"a\x1b[8mhidden", "a\x1b[0;8mhidden", "a\x1b[1;8;31mhidden"} {
		if got := Styled(in); strings.Contains(got, "\x1b[") {
			t.Errorf("Styled(%q) = %q keeps a conceal sequence", in, got)
		}
	}
	for _, in := range []string{"\x1b[38;5;8mgrey\x1b[0m", "\x1b[38;2;8;8;8mdark\x1b[0m", "\x1b[48;2;1;8;3mx\x1b[m"} {
		if got := Styled(in); got != in {
			t.Errorf("Styled(%q) = %q, want it kept", in, got)
		}
	}
}

// Aikido review on #426: conceal written in the colon (sub-parameter) form
// is conceal too; other colon forms (extended colors, underline styles)
// stay.
func TestStyledDropsColonFormConceal(t *testing.T) {
	for _, in := range []string{"a\x1b[8:1mhidden", "a\x1b[08:0mhidden", "a\x1b[1;8:mhidden", "a\x1b[38:5:1;8:2mhidden"} {
		if got := Styled(in); strings.Contains(got, "\x1b[") {
			t.Errorf("Styled(%q) = %q keeps a conceal sequence", in, got)
		}
	}
	for _, in := range []string{"\x1b[38:5:8mgrey\x1b[0m", "\x1b[38:2::8:8:8mdark\x1b[0m", "\x1b[4:3mcurly\x1b[4:0m", "\x1b[58:5:8mx\x1b[m"} {
		if got := Styled(in); got != in {
			t.Errorf("Styled(%q) = %q, want it kept", in, got)
		}
	}
}
