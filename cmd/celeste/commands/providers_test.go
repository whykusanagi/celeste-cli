package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

func providersOutput(t *testing.T, args ...string) string {
	t.Helper()
	res := HandleProvidersCommand(&Command{Name: "providers", Args: args}, &CommandContext{})
	require.True(t, res.Success, res.Message)
	return res.Message
}

// section returns the lines under heading up to the next blank line.
func section(out, heading string) []string {
	var lines []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, heading):
			in = true
		case in && strings.TrimSpace(l) == "":
			return lines
		case in:
			lines = append(lines, l)
		}
	}
	return lines
}

// #151: every provider's info page fills every section.
func TestProviderInfoHasNoEmptySection(t *testing.T) {
	for _, name := range providers.ListProviders() {
		out := providersOutput(t, "info", name)
		assert.Contains(t, out, "API Endpoint:", name)
		for _, h := range []string{"MODELS:", "AUTHENTICATION:", "TEST STATUS:", "EXAMPLE USAGE:"} {
			assert.NotEmpty(t, section(out, h), "%s: %s is empty", name, h)
		}
	}
}

// #151: the local example uses a named profile and no real key.
func TestProviderInfoLocalExample(t *testing.T) {
	out := providersOutput(t, "info", "local")
	assert.Contains(t, out, "API Endpoint:  your own, for example http://127.0.0.1:8080/v1")
	assert.Contains(t, out, "celeste config -config local --set-url http://127.0.0.1:8080/v1")
	assert.Contains(t, out, "celeste config -config local --set-key not-needed")
	assert.Contains(t, out, "celeste -config local chat")
	assert.NotContains(t, out, "YOUR_API_KEY")
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "celeste config ") {
			assert.Contains(t, l, "-config local", "example edits the default profile: %q", l)
		}
	}
}

// Every example names its profile, so none writes the default profile.
func TestProviderInfoExamplesUseNamedProfile(t *testing.T) {
	for _, name := range providers.ListProviders() {
		for _, l := range section(providersOutput(t, "info", name), "EXAMPLE USAGE:") {
			if strings.Contains(l, "celeste config ") {
				assert.Contains(t, l, "-config "+name, "%s: %q", name, l)
			}
		}
	}
}

// #151: Venice is per model, not "[NO TOOLS]"; Vertex's default is labelled.
func TestProvidersListLabels(t *testing.T) {
	out := providersOutput(t)
	line := func(name string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, " "+name+" ") {
				return l
			}
		}
		t.Fatalf("no line for %s in:\n%s", name, out)
		return ""
	}
	assert.Contains(t, line("venice"), "[PER MODEL]")
	assert.Contains(t, line("vertex"), "gemini-2.0-flash (preferred) (unverified)")
	assert.NotContains(t, line("gemini"), "unverified")
	assert.Contains(t, out, "Total: 11 providers: 8 with tools, 1 where it depends on the model")

	tools := providersOutput(t, "--tools")
	assert.Contains(t, tools, "Total: 8 tool-capable providers, plus 1 where it depends on the model")
	assert.Contains(t, tools, "gemini-2.0-flash (unverified)")

	info := providersOutput(t, "info", "venice")
	assert.Contains(t, info, "Function Calling:    ◐ Per model")
	vertex := providersOutput(t, "info", "vertex")
	assert.Contains(t, vertex, "Default:          gemini-2.0-flash (unverified")
	assert.Contains(t, vertex, "Application Default Credentials")
}
