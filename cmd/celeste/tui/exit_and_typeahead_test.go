package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/commands"
)

// The header matches the real exit behavior: Ctrl+C cancels a running turn
// and a second press within 3s quits, so one press never exits.
func TestHeaderSaysCtrlCTwiceToExit(t *testing.T) {
	out := NewHeaderModel().SetWidth(200).View()
	if !strings.Contains(out, "Ctrl+C twice to exit") {
		t.Fatalf("header should say Ctrl+C twice to exit, got %q", out)
	}
}

// Picking "exit" in /menu quits like typing exit does; it must not be sent
// as /exit, which is not a slash command (#314).
func TestMenuExitQuits(t *testing.T) {
	m := AppModel{viewMode: "menu"}
	_, cmd := m.Update(menuItemSelectedMsg{command: "exit"})
	if cmd == nil {
		t.Fatal("menu exit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("menu exit should quit")
	}
}

// Every slash command name either dispatch table accepts is offered by
// typeahead, aliases included, so /help never advertises a command that
// typeahead cannot complete.
func TestTypeaheadOffersEveryDispatchedName(t *testing.T) {
	for _, clauses := range [][][]string{
		switchCaseNames(t, "app.go", "cmd.Name"),
		switchCaseNames(t, "../commands/commands.go", "strings.ToLower(cmd.Name)"),
	} {
		for _, clause := range clauses {
			for _, name := range clause {
				if !contains(knownCommands, name) {
					t.Errorf("knownCommands lacks /%s", name)
				}
			}
		}
	}
}

// NSFW /help lists /set-model once as a command line, in the shared chat
// command list, not again in an NSFW-only block.
func TestNSFWHelpListsSetModelOnce(t *testing.T) {
	res := commands.Execute(&commands.Command{Name: "help"}, &commands.CommandContext{NSFWMode: true})
	n := 0
	for _, line := range strings.Split(res.Message, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "/set-model") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("NSFW /help lists /set-model as a command %d times, want 1", n)
	}
}
