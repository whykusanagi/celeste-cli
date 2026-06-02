package llm

import "testing"

func TestValidateToolArgs(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		args    string
		wantErr bool
	}{
		{"empty args is ok for no-arg tools", "noop", "", false},
		{"valid json object", "speak", `{"text":"hello"}`, false},
		{"valid json with nested", "speak", `{"text":"hi","ssml":true}`, false},
		{"truncated object", "speak", `{"text":"hel`, true},
		{"garbage", "speak", `{text:`, true},
		{"bare word", "speak", `text`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := validateToolArgs(c.tool, c.args)
			if (got != "") != c.wantErr {
				t.Fatalf("validateToolArgs(%q, %q) = %q; wantErr=%v", c.tool, c.args, got, c.wantErr)
			}
		})
	}
}

func TestValidateToolArgs_FlagsCorruptedThenValid(t *testing.T) {
	intact := `{"text":"a very long script that exercises multi-chunk streaming","ssml":false}`
	truncated := `{"text":"a very long script that exercises multi-ch`
	if got := validateToolArgs("speak", intact); got != "" {
		t.Fatalf("intact payload should be valid, got %q", got)
	}
	if got := validateToolArgs("speak", truncated); got == "" {
		t.Fatalf("truncated payload should be flagged")
	}
}
