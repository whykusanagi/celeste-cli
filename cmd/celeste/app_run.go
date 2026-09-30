package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

type commandRunner interface {
	PrintUsage()
	HasDefaultConfig() bool
	RunChat()
	RunConfig(args []string)
	RunSingleMessage(message string)
	RunContext(args []string)
	RunStats(args []string)
	RunExport(args []string)
	RunSkill(args []string)
	RunWalletMonitor(args []string)
	RunSkills(args []string)
	RunProviders(args []string)
	RunSession(args []string)
	RunCollections(args []string)
	RunAgent(args []string)
	RunInit(args []string)
	RunGrimoire(args []string)
	RunIndex(args []string)
	RunServe(args []string)
	RunCosts(args []string)
	RunMemories(args []string)
	RunRemember(args []string)
	RunForget(args []string)
	RunResume(args []string)
	RunPlan(args []string)
	RunRevert(args []string)
	RunMCP(args []string)
	RunHooks(args []string)
}

type defaultCommandRunner struct{}

func (defaultCommandRunner) PrintUsage()             { printUsage() }
func (defaultCommandRunner) HasDefaultConfig() bool  { return hasDefaultConfig() }
func (defaultCommandRunner) RunChat()                { runChatTUI() }
func (defaultCommandRunner) RunConfig(args []string) { runConfigCommand(args) }
func (defaultCommandRunner) RunSingleMessage(message string) {
	runSingleMessage(message)
}
func (defaultCommandRunner) RunContext(args []string)       { runContextCommand(args) }
func (defaultCommandRunner) RunStats(args []string)         { runStatsCommand(args) }
func (defaultCommandRunner) RunExport(args []string)        { runExportCommand(args) }
func (defaultCommandRunner) RunSkill(args []string)         { runSkillExecuteCommand(args) }
func (defaultCommandRunner) RunWalletMonitor(args []string) { runWalletMonitorCommand(args) }
func (defaultCommandRunner) RunSkills(args []string)        { runSkillsCommand(args) }
func (defaultCommandRunner) RunProviders(args []string)     { runProvidersCommand(args) }
func (defaultCommandRunner) RunSession(args []string)       { runSessionCommand(args) }
func (defaultCommandRunner) RunCollections(args []string)   { runCollectionsCommand(args) }
func (defaultCommandRunner) RunAgent(args []string)         { runAgentCommand(args) }
func (defaultCommandRunner) RunInit(args []string)          { runInitCommand(args) }
func (defaultCommandRunner) RunGrimoire(args []string)      { runGrimoireCommand(args) }
func (defaultCommandRunner) RunIndex(args []string)         { runIndexCommand(args) }
func (defaultCommandRunner) RunServe(args []string)         { runServeCommand(args) }
func (defaultCommandRunner) RunCosts(args []string)         { runCostsCommand(args) }
func (defaultCommandRunner) RunMemories(args []string)      { runMemoriesCommand(args) }
func (defaultCommandRunner) RunRemember(args []string)      { runRememberCommand(args) }
func (defaultCommandRunner) RunForget(args []string)        { runForgetCommand(args) }
func (defaultCommandRunner) RunResume(args []string)        { runResumeCommand(args) }
func (defaultCommandRunner) RunPlan(args []string)          { runPlanCommand(args) }
func (defaultCommandRunner) RunRevert(args []string)        { runRevertCommand(args) }
func (defaultCommandRunner) RunMCP(args []string)           { runMCPCommand(args) }
func (defaultCommandRunner) RunHooks(args []string)         { runHooksCommand(args) }

func main() {
	os.Exit(run(os.Args[1:], defaultCommandRunner{}, os.Stdout, os.Stderr))
}

func run(args []string, runner commandRunner, stdout, stderr io.Writer) int {
	resetGlobalFlags()
	args = extractGlobalFlags(args)

	if len(args) < 1 {
		// No args = launch TUI chat (like `claude` with no args)
		runner.RunChat()
		return 0
	}

	command := args[0]
	cmdArgs := args[1:]

	switch command {
	case "chat":
		runner.RunChat()
	case "config":
		runner.RunConfig(cmdArgs)
	case "message", "msg":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, "Usage: celeste message <text>")
			return 1
		}
		runner.RunSingleMessage(strings.Join(cmdArgs, " "))
	case "context":
		runner.RunContext(cmdArgs)
	case "stats":
		runner.RunStats(cmdArgs)
	case "export":
		runner.RunExport(cmdArgs)
	case "skill":
		runner.RunSkill(cmdArgs)
	case "wallet-monitor":
		runner.RunWalletMonitor(cmdArgs)
	case "skills":
		runner.RunSkills(cmdArgs)
	case "providers":
		runner.RunProviders(cmdArgs)
	case "session", "sessions":
		runner.RunSession(cmdArgs)
	case "collections":
		runner.RunCollections(cmdArgs)
	case "agent":
		runner.RunAgent(cmdArgs)
	case "init":
		runner.RunInit(cmdArgs)
	case "grimoire":
		runner.RunGrimoire(cmdArgs)
	case "index":
		runner.RunIndex(cmdArgs)
	case "serve":
		runner.RunServe(cmdArgs)
	case "costs":
		runner.RunCosts(cmdArgs)
	case "memories":
		runner.RunMemories(cmdArgs)
	case "remember":
		runner.RunRemember(cmdArgs)
	case "forget":
		runner.RunForget(cmdArgs)
	case "resume":
		runner.RunResume(cmdArgs)
	case "plan":
		runner.RunPlan(cmdArgs)
	case "revert":
		runner.RunRevert(cmdArgs)
	case "mcp":
		runner.RunMCP(cmdArgs)
	case "hooks":
		runner.RunHooks(cmdArgs)
	case "help", "-h", "--help":
		runner.PrintUsage()
	case "version", "-v", "--version":
		if CommitSHA != "dev" {
			fmt.Fprintf(stdout, "Celeste CLI %s (%s) [%s]\n", Version, Build, shortCommit(CommitSHA))
		} else {
			fmt.Fprintf(stdout, "Celeste CLI %s (%s)\n", Version, Build)
		}
	default:
		// A lone lowercase word that is a guessed command (`celeste
		// models`) or a near-miss of one (`celeste confg`) errors with a
		// suggestion instead of reaching the model (#151). Any other lone
		// word (`celeste hello`), and anything longer, is a message.
		if len(args) == 1 && looksLikeCommand(command) && closestCommand(command) != "" {
			fmt.Fprint(stderr, unknownCommandMessage(command))
			return 1
		}
		runner.RunSingleMessage(strings.Join(args, " "))
	}

	return 0
}

// knownCommands are the words run dispatches, for suggestions. Keep it in
// step with run's switch (TestKnownCommandsDispatch checks it).
var knownCommands = []string{
	"agent", "chat", "collections", "config", "context", "costs", "export",
	"forget", "grimoire", "help", "hooks", "index", "init", "mcp", "memories",
	"message", "plan", "providers", "remember", "resume", "revert", "serve",
	"session", "sessions", "skill", "skills", "stats", "version", "wallet-monitor",
}

// commandHints answer words people guess that are not commands. They and
// their near-misses error like a mistyped command.
var commandHints = map[string]string{
	"models": "celeste providers    (each provider's default model; /set-model lists models inside chat)",
	"model":  "celeste providers    (each provider's default model; /set-model lists models inside chat)",
	"status": "celeste config       (the active profile, provider and model)",
}

func looksLikeCommand(word string) bool {
	if word == "" || word[0] < 'a' || word[0] > 'z' {
		return false
	}
	for _, r := range word {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// unknownCommandMessage is the error for a lone word closestCommand
// matched: the closest command (or a guessed word's hint), how to send the
// word as a message, and where the command list is.
func unknownCommandMessage(word string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Unknown command %q.\n", word)
	if best := closestCommand(word); commandHints[best] != "" {
		fmt.Fprintf(&b, "Did you mean: %s\n", commandHints[best])
	} else if best != "" {
		fmt.Fprintf(&b, "Did you mean: celeste %s\n", best)
	}
	fmt.Fprintf(&b, "To send it to Celeste as a message: celeste message %s\n", word)
	b.WriteString("Run celeste help for the command list.\n")
	return b.String()
}

// closestCommand returns the guessed word (a commandHints key) or known
// command nearest to word, or "" when none is close: at most 1 edit for words
// of up to 5 letters, 2 for longer ones ("hello" must not become "help").
// Ties go to a guessed word ("stauts" is status, not stats), then to the
// first in knownCommands.
func closestCommand(word string) string {
	limit := 1
	if len(word) > 5 {
		limit = 2
	}
	best, bestDist := "", limit+1
	for _, c := range append(hintWords(), knownCommands...) {
		if d := editDistance(word, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// hintWords is commandHints' keys, sorted, so closestCommand is deterministic.
func hintWords() []string {
	words := make([]string, 0, len(commandHints))
	for w := range commandHints {
		words = append(words, w)
	}
	sort.Strings(words)
	return words
}

// editDistance is the optimal-string-alignment distance between a and b:
// insertions, deletions, substitutions and adjacent swaps each cost 1
// ("agnet" is 1 from "agent"). By bytes: looksLikeCommand admits only ASCII.
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}

func resetGlobalFlags() {
	configName = ""
	runtimeModeOverride = ""
	clawMaxToolIterationsOverride = 0
}

func extractGlobalFlags(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "-config" && i+1 < len(args) {
			configName = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-config=") {
			configName = strings.TrimPrefix(args[i], "-config=")
			continue
		}

		if args[i] == "-mode" && i+1 < len(args) {
			runtimeModeOverride = strings.ToLower(strings.TrimSpace(args[i+1]))
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-mode=") {
			runtimeModeOverride = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(args[i], "-mode=")))
			continue
		}

		if args[i] == "-claw-max-iterations" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				clawMaxToolIterationsOverride = n
				i++
				continue
			}
		}
		if strings.HasPrefix(args[i], "-claw-max-iterations=") {
			raw := strings.TrimPrefix(args[i], "-claw-max-iterations=")
			if n, err := strconv.Atoi(raw); err == nil {
				clawMaxToolIterationsOverride = n
				continue
			}
		}

		filtered = append(filtered, args[i])
	}
	return filtered
}

// shortCommit renders a build stamp for display. CI stamps a full 40-char git
// SHA, which is abbreviated; `make install` stamps an already-short value like
// "4078dec" or "4078dec-dirty", which is kept whole so the dirty marker stays
// visible. Previously this was CommitSHA[:8], which panicked on any stamp
// shorter than 8 characters and truncated "4078dec-dirty" to "4078dec-".
// Length alone is too weak a test: a 40-character value that is not a SHA (a
// long describe stamp, or a tag like "release-2026-08-08-build-metadata-abcdef")
// would be cut to 8 misleading characters — the same defect this helper fixes.
// Require hex as well, which only a real SHA satisfies.
func shortCommit(sha string) string {
	const fullSHALen = 40
	if len(sha) == fullSHALen && isHex(sha) {
		return sha[:8]
	}
	return sha
}

// isHex reports whether s is entirely hexadecimal digits.
func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9',
			r >= 'a' && r <= 'f',
			r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
