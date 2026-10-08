package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// A failing custom tool's result names the tool, never the configured
// command: a credential written into the command line is not sent to the
// model.
func TestCustomToolFailureDoesNotEchoTheCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	secret := "token-0123456789abcdef"
	c := &customToolWrapper{name: "deploy", command: "echo out; exit 3 # " + secret}
	res, err := c.Execute(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Error {
		t.Fatalf("want an error result, got %q", res.Content)
	}
	if strings.Contains(res.Content, secret) || strings.Contains(res.Content, "exit 3") {
		t.Fatalf("the failure echoes the configured command: %q", res.Content)
	}
	if !strings.Contains(res.Content, `"deploy"`) || !strings.Contains(res.Content, "exit status 3") || !strings.Contains(res.Content, "out") {
		t.Fatalf("the failure should name the tool, the status and the output: %q", res.Content)
	}
}
