package main

import "testing"

func TestMaskKeyNeverExposesCredentialBytes(t *testing.T) {
	for _, key := range []string{"", "x", "abcd", "sk-sensitive-1234567890"} {
		got := maskKey(key)
		if key == "" && got != "(not set)" {
			t.Fatalf("empty key: got %q", got)
		}
		if key != "" && got != "set" {
			t.Fatalf("configured key: got %q", got)
		}
	}
}
