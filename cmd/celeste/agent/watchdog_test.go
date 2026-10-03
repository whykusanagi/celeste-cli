package agent

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// The watchdog's oracle is built only when the watchdog is on or in
// shadow: off reads no key and prints nothing (2.0 W3).
func TestWatchdogOracleOnlyWhenTheWatchdogRuns(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var logged []string
	logf := func(s string) { logged = append(logged, s) }
	if o := WatchdogOracle(&config.Config{Oracle: "jev"}, "", logf); o != nil || len(logged) != 0 {
		t.Errorf("watchdog off: oracle %v, logged %v", o, logged)
	}
	if WatchdogOracle(nil, "", logf) != nil {
		t.Error("no config: no oracle")
	}
	if o := WatchdogOracle(&config.Config{Watchdog: "shadow", Oracle: "jev"}, "", logf); o == nil || len(logged) != 1 {
		t.Errorf("watchdog shadow without a key: oracle %v, logged %v (want the heuristic and one line)", o, logged)
	}
	if o := WatchdogOracle(&config.Config{Watchdog: "on", Oracle: "llm", APIKey: "k", BaseURL: "http://127.0.0.1:1"}, "", logf); o == nil {
		t.Error("watchdog on with oracle llm: no oracle")
	}
}
