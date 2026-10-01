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

// #151, I4 (#144 W6b review): the local example uses a named profile and no
// key at all — needsAPIKey (#151) means celeste never asks for one here,
// so the example must not tell the user to set a placeholder.
func TestProviderInfoLocalExample(t *testing.T) {
	out := providersOutput(t, "info", "local")
	assert.Contains(t, out, "API Endpoint:  your own, for example http://127.0.0.1:8080/v1")
	assert.Contains(t, out, "celeste config -config local --set-url http://127.0.0.1:8080/v1")
	assert.Contains(t, out, "celeste -config local chat")
	assert.NotContains(t, out, "YOUR_API_KEY")
	assert.NotContains(t, out, "--set-key", "no key is needed at all, so there is nothing to set")
	assert.NotContains(t, out, "not-needed")
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "celeste config ") {
			assert.Contains(t, l, "-config local", "example edits the default profile: %q", l)
		}
	}
}

// I4: the AUTHENTICATION section must not claim celeste still needs a
// non-empty api_key for a provider whose registry entry needs no key at
// all (needsAPIKey, #151).
func TestProviderInfoLocalAuthenticationSaysNoKeyNeeded(t *testing.T) {
	out := providersOutput(t, "info", "local")
	section := section(out, "AUTHENTICATION:")
	joined := strings.Join(section, "\n")
	assert.Contains(t, joined, "Required: none")
	assert.NotContains(t, joined, "api_key")
	assert.NotContains(t, joined, "placeholder")
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

// #151, W6b: Vertex's default is labelled. Venice is [PER MODEL]: the chat
// gates tools per selected model now, so it is no longer a blanket
// [NO TOOLS].
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
	assert.NotContains(t, line("venice"), "[NO TOOLS]")
	assert.Contains(t, line("vertex"), "gemini-2.0-flash (preferred) (unverified)")
	assert.NotContains(t, line("gemini"), "unverified")
	assert.Contains(t, out, "Total: 11 providers: 8 with tools, 1 where it depends on the model\n")

	tools := providersOutput(t, "--tools")
	assert.Contains(t, tools, "Total: 8 tool-capable providers, plus 1 where it depends on the model\n")
	assert.Contains(t, tools, "Tools depend on the model (checked against the live catalogue):\n  venice\n")
	assert.Contains(t, tools, "gemini-2.0-flash (unverified)")

	info := providersOutput(t, "info", "venice")
	assert.Contains(t, info, "Function Calling:    ◐ Per model (checked against the live catalogue)")
	assert.Contains(t, info, "Model-dependent tool support")
	assert.Contains(t, info, "llama-3.3-70b: supports tools")
	vertex := providersOutput(t, "info", "vertex")
	assert.Contains(t, vertex, "Default:          gemini-2.0-flash (unverified")
	assert.Contains(t, vertex, "Application Default Credentials")
}

// Vertex authenticates with ADC. Any non-empty api_key would be sent as a
// Gemini API key instead (llm/backend_google.go), so the example must not
// set one.
func TestProviderInfoVertexUsesADC(t *testing.T) {
	out := providersOutput(t, "info", "vertex")
	assert.Contains(t, out, "celeste config -config vertex --use-google-adc\n")
	assert.Contains(t, out, `"google_use_adc": true`)
	assert.NotContains(t, out, "--set-key")
	assert.NotContains(t, out, "api_key")
	assert.NotContains(t, out, "not-needed")
}

// The info page must not contradict the registry's DigitalOcean default.
func TestProviderInfoDigitalOceanModel(t *testing.T) {
	out := providersOutput(t, "info", "digitalocean")
	do, _ := providers.GetProvider("digitalocean")
	assert.Contains(t, out, "Default:          "+do.DefaultModel)
	assert.NotContains(t, out, "gpt-4o-mini")
}
