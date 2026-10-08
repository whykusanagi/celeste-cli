package builtin

import "testing"

// A file name handed to an external program as a positional argument is
// never read as one of its options.
func TestPositionalFileNeverAnOption(t *testing.T) {
	cases := map[string]string{
		"-version":       "./-version",
		"--output=x.mp3": "./--output=x.mp3",
		"clip.mp3":       "clip.mp3",
		"/abs/clip.mp3":  "/abs/clip.mp3",
		"./-x.mp3":       "./-x.mp3",
	}
	for in, want := range cases {
		if got := positionalFile(in); got != want {
			t.Errorf("positionalFile(%q) = %q, want %q", in, got, want)
		}
	}
}
