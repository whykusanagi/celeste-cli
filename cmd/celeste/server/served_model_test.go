package server

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// An MCP chat request configured on a retired model runs on the served one;
// the shared config is not changed.
func TestMCPChatUsesResolvedModel(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, fp)
	// The fake provider listens on 127.0.0.1 ("local"); give it a catalog.
	defer providers.SetCatalogForTest("local", []providers.CatalogModel{{ID: "fake-model-v2", Default: true}})()
	srv := chatServer(t, cfg)

	out, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, "hi", ws)
	if err != nil || len(out) == 0 || !strings.HasPrefix(out[0].Text, "ok") {
		t.Fatalf("%v %+v", err, out)
	}
	if got := fp.Requests()[0].Body["model"]; got != "fake-model-v2" {
		t.Errorf("model sent = %v, want fake-model-v2", got)
	}
	if cfg.CelesteConfig.Model != "fake-model" {
		t.Errorf("the shared config changed to %q", cfg.CelesteConfig.Model)
	}
}
