package main

import (
	"errors"
	"fmt"
	"io"
	"os"
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
	args, err := extractGlobalFlags(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 2
	}

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
		// A lone word people guess for a command (`celeste models`) errors
		// with a hint instead of reaching the model (#151). Every other
		// input, a lone word included, is a message.
		if _, guessed := commandHints[command]; guessed && len(args) == 1 {
			fmt.Fprint(stderr, unknownCommandMessage(command))
			return 1
		}
		runner.RunSingleMessage(strings.Join(args, " "))
	}

	return 0
}

// commandHints answer the lone words people guess for a command. Only these
// exact words error; there is no typo matching, so any other word chats.
var commandHints = map[string]string{
	"models": "celeste providers    (each provider's default model; /set-model lists models inside chat)",
	"model":  "celeste providers    (each provider's default model; /set-model lists models inside chat)",
	"status": "celeste config       (the active profile, provider and model)",
}

// unknownCommandMessage is the error for a lone guessed word: its hint, how
// to send the word as a message, and where the command list is.
func unknownCommandMessage(word string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Unknown command %q.\n", word)
	fmt.Fprintf(&b, "Did you mean: %s\n", commandHints[word])
	fmt.Fprintf(&b, "To send it to Celeste as a message: celeste message %s\n", word)
	b.WriteString("Run celeste help for the command list.\n")
	return b.String()
}

func resetGlobalFlags() {
	configName = ""
	maxToolIterationsOverride = 0
}

// errModeFlagRemoved answers -mode (#144, spec §6.2).
var errModeFlagRemoved = errors.New("the -mode flag was removed in celeste 2.0: chat always runs tools in a loop, so drop the flag. See MIGRATING-2.0.md")

// iterFlag reports whether args[i] is -max-tool-iterations or the deprecated
// -claw-max-iterations with a numeric value: the value, how many extra args
// it consumed, and whether it was the legacy name. A non-numeric value is
// not a match, so the args pass through as before.
func iterFlag(args []string, i int) (n, extra int, legacy, ok bool) {
	for _, name := range []string{"-max-tool-iterations", "-claw-max-iterations"} {
		raw := ""
		switch {
		case args[i] == name && i+1 < len(args):
			raw, extra = args[i+1], 1
		case strings.HasPrefix(args[i], name+"="):
			raw, extra = strings.TrimPrefix(args[i], name+"="), 0
		default:
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, false, false
		}
		return v, extra, name == "-claw-max-iterations", true
	}
	return 0, 0, false, false
}

func extractGlobalFlags(args []string, stderr io.Writer) ([]string, error) {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" && i+1 < len(args):
			configName = args[i+1]
			i++
			continue
		case strings.HasPrefix(a, "-config="):
			configName = strings.TrimPrefix(a, "-config=")
			continue
		case a == "-mode" || a == "--mode" || strings.HasPrefix(a, "-mode=") || strings.HasPrefix(a, "--mode="):
			return nil, errModeFlagRemoved
		}
		if n, extra, legacy, ok := iterFlag(args, i); ok {
			if legacy {
				fmt.Fprintln(stderr, "Warning: -claw-max-iterations is deprecated; use -max-tool-iterations (see MIGRATING-2.0.md)")
			}
			maxToolIterationsOverride = n
			i += extra
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered, nil
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
