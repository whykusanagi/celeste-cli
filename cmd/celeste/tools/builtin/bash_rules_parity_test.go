package builtin

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
)

// The advisory destructive-bash rule and the watchdog's unsafe heuristic
// (both rules.Destructive) must see every rm this blocking check refuses:
// they read the same shell (internal/shellparse) and the same rm policy.
func TestRulesDestructiveSeesEveryBlockedRm(t *testing.T) {
	tooDeep := "rm -rf /usr"
	for i := 0; i < 8; i++ {
		tooDeep = "eval " + tooDeep
	}
	lists := [][]string{rmEvasionCases, rmStillBlockedCases, rmIFSCases, shellOptionFormCases,
		parserDisagreementCases, recursiveWithoutForceCases, {tooDeep, strings.Repeat("eval ", 8) + "true"}}
	for _, list := range lists {
		for _, cmd := range list {
			if checkDangerousCommand(cmd) == "" {
				t.Errorf("shared case no longer blocked by the bash tool: %q", cmd)
				continue
			}
			if !rules.Destructive(cmd) {
				t.Errorf("blocked by the bash tool, missed by rules.Destructive: %q", cmd)
			}
		}
	}
}
