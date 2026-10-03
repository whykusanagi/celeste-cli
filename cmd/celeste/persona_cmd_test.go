package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Review Focus 8: a binary without the key fails verification, which is
// what fails a release built without the secret.
func TestPersonaVerifyFailsWithoutTheKey(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"persona", "verify"}, &fakeRunner{}, &out, &errBuf); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errBuf.String(), "public persona only") || !strings.Contains(errBuf.String(), "no persona key") {
		t.Fatalf("stderr = %q", errBuf.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestPersonaVerifyPassesWithThePersonaDecrypted(t *testing.T) {
	useTestPersona(t)
	var out, errBuf bytes.Buffer
	if code := run([]string{"persona", "verify"}, &fakeRunner{}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	if !strings.HasPrefix(out.String(), "official persona: core ") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestPersonaCommandUsage(t *testing.T) {
	for _, args := range [][]string{{"persona"}, {"persona", "check"}, {"persona", "verify", "extra"}} {
		var out, errBuf bytes.Buffer
		if code := run(args, &fakeRunner{}, &out, &errBuf); code != 2 || !strings.Contains(errBuf.String(), "Usage: celeste persona verify") {
			t.Fatalf("%q: exit %d, stderr %q", args, code, errBuf.String())
		}
	}
}

// `celeste help` lists the command.
func TestUsageListsPersonaVerify(t *testing.T) {
	if !strings.Contains(usageText, "persona verify") {
		t.Fatal("usage does not list persona verify")
	}
}

// The notice outside the chat prints once per process (ruling 17).
func TestPersonaNoticePrintsOnce(t *testing.T) {
	personaNoticeOnce = sync.Once{} // fresh for this test
	t.Cleanup(func() { personaNoticeOnce = sync.Once{} })
	var errBuf bytes.Buffer
	printPersonaNoticeOnce(&errBuf)
	printPersonaNoticeOnce(&errBuf)
	if n := strings.Count(errBuf.String(), "public persona"); n != 1 {
		t.Fatalf("printed %d times: %q", n, errBuf.String())
	}
}

// An official build prints nothing.
func TestPersonaNoticeSilentWhenOfficial(t *testing.T) {
	useTestPersona(t)
	personaNoticeOnce = sync.Once{}
	t.Cleanup(func() { personaNoticeOnce = sync.Once{} })
	var errBuf bytes.Buffer
	printPersonaNoticeOnce(&errBuf)
	if errBuf.Len() != 0 {
		t.Fatalf("official build printed %q", errBuf.String())
	}
}

// Review Focus 7: with the public persona active, the request carries the
// public text and nothing that looks like ciphertext.
func TestFallbackRequestCarriesThePublicPersona(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	c := llm.NewClient(&llm.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "gpt-4.1", Timeout: 10 * time.Second}, nil)
	c.SetSystemPrompt(prompts.Compose(prompts.ComposeOptions{Mode: prompts.ModeChat}).String())
	if _, err := c.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "hi", Timestamp: time.Now()}}, nil); err != nil {
		t.Fatal(err)
	}
	sys := srv.Requests()[0].Body["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(sys, "You are Celeste, the AI companion in the celeste command-line tool") {
		t.Fatalf("system prompt starts %.80q", sys)
	}
	if strings.Contains(sys, "CPv1") {
		t.Fatal("ciphertext reached the request")
	}
}
