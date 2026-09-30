package builtin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// The numbers README and docs/ quote, computed from the code (#151). Each
// claim below is a pattern whose first group must equal one of these, and
// which must match at least once, so rewording a claim away fails here
// instead of drifting silently.
//
// Tool counts are the built-in surface: RegisterAll (41 always on) plus the
// 6 code-graph tools and collections search. The chat also offers
// spawn_agent and post_message (registered by the chat, not here), and any
// MCP server or custom skill adds more; the docs say so in words.
//
// Provider counts: "registered" is every Registry entry (what `celeste
// providers` lists); "chat providers" are the eight with tool calling
// (SupportsFunctionCalling) plus Venice (ToolsPerModel), which chats without
// tools until per-model tool gating is wired into the chat (W6b).
type docTruths struct {
	allTools, coreTools, codegraphTools, chatTools, agentTools int
	registered, withTools, perModel, chatProviders             int
}

func computeTruths(t *testing.T) docTruths {
	t.Helper()
	r := tools.NewRegistry()
	RegisterAll(r, t.TempDir(), countingConfigLoader{}, nil, nil)
	core := r.Count()
	RegisterCodeGraphTools(r, nil)
	codegraph := r.Count() - core
	RegisterCollectionsTools(r, &config.Config{APIKey: "k", Collections: &config.CollectionsConfig{ActiveCollections: []string{"c"}}})
	withTools := len(providers.GetToolCallingProviders())
	perModel := len(providers.GetPerModelToolProviders())
	return docTruths{
		allTools:       r.Count(),
		coreTools:      core,
		codegraphTools: codegraph,
		chatTools:      len(r.GetTools(tools.ModeChat)),
		agentTools:     len(r.GetTools(tools.ModeAgent)),
		registered:     len(providers.ListProviders()),
		withTools:      withTools,
		perModel:       perModel,
		chatProviders:  withTools + perModel,
	}
}

// repoFile reads a file relative to the repository root
// (cmd/celeste/tools/builtin is four levels down).
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		// ponytail: the Docker job runs a prebuilt test binary with no repo
		// checkout; the docs check runs in the regular test job instead.
		t.Skipf("docs not reachable from here (%s); run from a repo checkout", rel)
	}
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

type claim struct {
	file    string
	pattern string // first group is the number
	want    func(docTruths) int
	what    string
}

func TestDocsClaimsMatchCode(t *testing.T) {
	truth := computeTruths(t)
	all := func(d docTruths) int { return d.allTools }
	claims := []claim{
		{"README.md", `🔮 \*\*(\d+) Built-in Tools\*\*`, all, "built-in tools"},
		{"README.md", `Tool System \((\d+) Tools\)`, all, "built-in tools"},
		{"README.md", `#-tool-system-(\d+)-tools`, all, "built-in tools (anchor)"},
		{"README.md", `\*\*(\d+) built-in tools\*\* powered`, all, "built-in tools"},
		{"README.md", `(\d+) are always on`, func(d docTruths) int { return d.coreTools }, "always-on tools"},
		{"README.md", `A further (\d+)\s+code-graph tools`, func(d docTruths) int { return d.codegraphTools }, "code-graph tools"},
		{"README.md", `Chat offers all (\d+)`, func(d docTruths) int { return d.chatTools }, "chat-mode tools"},
		{"README.md", `Agent runs offer (\d+)`, func(d docTruths) int { return d.agentTools }, "agent-mode tools"},
		{"README.md", `\| \*\*Tools\*\* \| (\d+) \|`, all, "comparison table tools"},
		{"README.md", `Multi-Provider Support \((\d+) Chat Providers\)`, func(d docTruths) int { return d.chatProviders }, "chat providers"},
		{"README.md", `\| \*\*Providers\*\* \| (\d+) `, func(d docTruths) int { return d.chatProviders }, "comparison table providers"},
		{"README.md", "`celeste providers` lists (\\d+)", func(d docTruths) int { return d.registered }, "registered providers"},
		{"README.md", `\((\d+)-turn safety cap\)`, func(docTruths) int { return config.DefaultMaxToolIterations }, "chat turn cap"},
		{"docs/LLM_PROVIDERS.md", `supports \*\*(\d+) chat providers\*\*`, func(d docTruths) int { return d.chatProviders }, "chat providers"},
		{"docs/LLM_PROVIDERS.md", `my (\d+) tools`, all, "built-in tools"},
		{"docs/LLM_PROVIDERS.md", "`celeste chat` \\(TUI\\) \\| (\\d+) built-in", func(d docTruths) int { return d.chatTools }, "chat-mode tools"},
		{"docs/LLM_PROVIDERS.md", "`celeste agent` \\| (\\d+) built-in", func(d docTruths) int { return d.agentTools }, "agent-mode tools"},
		{"docs/PROVIDER_AUDIT_MATRIX.md", `(\d+) chat providers`, func(d docTruths) int { return d.chatProviders }, "chat providers"},
		{"docs/CAPABILITIES.md", `\*\*(\d+) dev-crushing tools\*\*`, all, "built-in tools"},
		{"docs/CAPABILITIES.md", `\*\*(\d+) Chat Providers:\*\*`, func(d docTruths) int { return d.chatProviders }, "chat providers"},
	}
	for _, c := range claims {
		matches := regexp.MustCompile(c.pattern).FindAllStringSubmatch(repoFile(t, c.file), -1)
		if len(matches) == 0 {
			t.Errorf("%s: no %s claim matches %q; update the doc and this test together", c.file, c.what, c.pattern)
			continue
		}
		want := c.want(truth)
		for _, m := range matches {
			if got, _ := strconv.Atoi(m[1]); got != want {
				t.Errorf("%s says %d %s (%q); the code has %d", c.file, got, c.what, m[0], want)
			}
		}
	}
}

// tableRows counts the rows of the first markdown table whose header line
// starts with header (separator line excluded).
func tableRows(t *testing.T, doc, header string) int {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, header) {
			continue
		}
		n := 0
		for _, row := range lines[i+2:] {
			if !strings.HasPrefix(row, "|") {
				break
			}
			n++
		}
		return n
	}
	t.Fatalf("no table starting %q", header)
	return 0
}

// The provider tables list exactly the chat providers.
func TestDocsProviderTablesMatchRegistry(t *testing.T) {
	truth := computeTruths(t)
	for file, header := range map[string]string{
		"docs/LLM_PROVIDERS.md":         "| Provider | Tools |",
		"docs/PROVIDER_AUDIT_MATRIX.md": "| Provider | Fn Calling |",
	} {
		if got := tableRows(t, repoFile(t, file), header); got != truth.chatProviders {
			t.Errorf("%s provider table has %d rows; the registry has %d chat providers", file, got, truth.chatProviders)
		}
	}
}

// #151: Sakana is the default; nothing may call Grok the default provider,
// and Vertex's shipped model is the registry's (labelled unverified).
func TestDocsDefaultProviderAndVertexModel(t *testing.T) {
	readme := repoFile(t, "README.md")
	for _, bad := range []string{`Grok/xAI \(default\)`, `Grok/xAI \(Default\)`, `Default config points to xAI`} {
		if regexp.MustCompile(bad).MatchString(readme) {
			t.Errorf("README still says %q; the default provider is %s", bad, config.DefaultProvider)
		}
	}
	seed, _ := providers.GetProvider(config.DefaultProvider)
	if !strings.Contains(readme, seed.Name+" (default)") {
		t.Errorf("README never names %q as the default", seed.Name+" (default)")
	}
	vertex, _ := providers.GetProvider("vertex")
	if !strings.Contains(readme, "**Google Vertex AI** ("+vertex.DefaultModel+", unverified)") {
		t.Errorf("README's Vertex line must name the shipped default %s, unverified", vertex.DefaultModel)
	}
	if !strings.Contains(repoFile(t, "docs/LLM_PROVIDERS.md"), "Vertex still ships `"+vertex.DefaultModel+"`") {
		t.Errorf("docs/LLM_PROVIDERS.md must carve Vertex out of the Gemini retirement note")
	}
}

// Venice's per-model tool support is not wired into the chat yet (the TUI
// disables skills for all of Venice), so no doc may promise it. W6b restores
// the wording when it wires ToolsPerModel into the chat.
func TestDocsDoNotPromiseVeniceTools(t *testing.T) {
	for _, file := range []string{"README.md", "docs/LLM_PROVIDERS.md", "docs/PROVIDER_AUDIT_MATRIX.md", "docs/CAPABILITIES.md"} {
		doc := repoFile(t, file)
		for _, bad := range []string{"Venice per model", "Per model", "tools per model", "Venice calls them", "Tool calling depends on the model"} {
			if strings.Contains(doc, bad) {
				t.Errorf("%s says %q, but the chat offers Venice no tools yet", file, bad)
			}
		}
	}
}
