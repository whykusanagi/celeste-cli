package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
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
	RunACP(args []string)
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
func (defaultCommandRunner) RunACP(args []string)           { runACPCommand(args) }

func main() {
	// A `go install` build becomes the official release binary first (W5
	// rulings 25–27); on success this does not return on unix.
	newUpgradeHook().beforeRun(os.Args)
	// A local server's reported context window replaces the 8,192 guess
	// (#310). Only the binary asks: tests talk to fake servers.
	config.EnableLocalWindowProbe()
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

	if usage, ok := subcommandUsage[command]; ok && len(cmdArgs) > 0 {
		if isHelpFlag(cmdArgs[0]) {
			fmt.Fprintln(stdout, usage)
			return 0
		}
		// -- ends the help check on the free-text subcommands, so text
		// starting with -h or --help is passed on; the -- itself is not.
		// Subcommands that parse flags see their -- unchanged.
		if cmdArgs[0] == "--" && freeTextSubcommands[command] {
			cmdArgs = cmdArgs[1:]
		}
	}

	switch command {
	case "chat":
		runner.RunChat()
	case "config":
		runner.RunConfig(cmdArgs)
	case "message", "msg":
		if len(cmdArgs) < 1 {
			fmt.Fprintln(stderr, messageUsage)
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
	case "acp":
		runner.RunACP(cmdArgs)
	case "update":
		return runUpdateCommand(cmdArgs, stdout, stderr)
	case "persona":
		return runPersonaCommand(cmdArgs, stdout, stderr)
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

const messageUsage = `Usage: celeste message <text>

Sends one message to Celeste and prints the reply (msg is short for it).
To send text that starts with -h or --help, put -- first:
  celeste message -- --help`

// freeTextSubcommands take free text or a name as their arguments and parse
// no flags; on these a leading -- ends the help check (#322).
var freeTextSubcommands = map[string]bool{
	"message": true, "msg": true, "remember": true, "forget": true, "resume": true, "skill": true,
}

// subcommandUsage holds the usage of every subcommand run() dispatches.
// -h, --help or -help as the first argument prints it on stdout, exits 0 and runs nothing (W-C1): the
// flag is never saved as a memory, looked up as a session, skill or memory
// name, or taken as an export format, and no index is built. Later
// arguments stay data, so a memory or a goal may mention --help. The usage
// of a subcommand with a flag set lists every flag it parses
// (TestSubcommandUsage_ListsEveryFlag).
var subcommandUsage = map[string]string{
	"message": messageUsage,
	"msg":     messageUsage,
	"chat": `Usage: celeste chat

Opens the chat UI (the same as celeste with no arguments).`,
	"config": `Usage: celeste [-config <name>] config [flags]

Shows or changes a config profile (~/.celeste/config.json, or
config.<name>.json with -config <name>).

Profiles:
  --show                     show the current configuration
  --list                     list all config profiles
  --init <provider>          create a profile (openai, grok, elevenlabs, venice, sakana, digitalocean)
  --set-default              make this profile the default when no -config is given
Connection:
  --set-key <key>            set the API key
  --set-url <url>            set the API URL
  --set-model <model>        set the model
  --set-max-tool-iterations N  set the chat's tool-loop turn cap
  --set-context-limit N      set the context window in tokens (0 uses the model default)
  --set-timeout <seconds>    fail a request after this long without data (0 = default: 60, or 600 for a local server)
  --set-management-key <key> set the xAI Management API key for collections
  --set-google-credentials <file>  set a Google Cloud service account JSON file
  --use-google-adc           use Google Application Default Credentials
Chat:
  --simulate-typing true|false  simulate typing
  --typing-speed N           typing speed in chars/sec (1-1000, default 60)
Skills (saved to skills.json):
  --set-tarot-token <token>  --set-tarot-url <url>  --set-venice-key <key>
  --set-weather-zip <zip>  --set-twitch-client-id <id>  --set-twitch-streamer <name>
  --set-youtube-key <key>  --set-youtube-channel <channel>`,
	"context": `Usage: celeste context [status|compact|reset]

Shows the token usage of the most recent session.`,
	"stats": `Usage: celeste stats

Shows usage statistics for the most recent session.`,
	"export": `Usage: celeste export [json|md|csv]
       celeste export <session-id> [json|md|csv]

Exports the most recent session, or the one given, as JSON (the default),
Markdown or CSV.`,
	"skill": `Usage: celeste skill <skill-name> [--arg value ...]

Runs one skill. Examples:
  celeste skill generate_uuid
  celeste skill get_weather --zip 90210
Use celeste skills --list to see the available skills.`,
	"wallet-monitor": `Usage: celeste wallet-monitor <start|stop|status>

Manages the wallet security monitoring daemon.
  start   start the daemon in the background
  stop    stop the running daemon
  status  show whether the daemon runs`,
	"skills": `Usage: celeste skills [flags]

Lists and manages skills.
  --list           list the available skills
  --info <name>    show a skill's details
  --exec <name>    run a skill
  --delete <name>  delete a skill
  --init           create the skills directory
  --reload         reload skills from disk`,
	"providers": `Usage: celeste providers [--tools|current|info <provider>]

Lists the AI providers and their default models.
  --tools          only the providers that support tool calls
  current          the provider of the current profile
  info <provider>  one provider's details`,
	"session":  sessionUsage,
	"sessions": sessionUsage,
	"collections": `Usage: celeste collections <list|create|upload|delete|enable|disable|show> [args]

Manages xAI collections (needs an xAI management key:
celeste config --set-management-key <key>).`,
	"agent": `Usage: celeste agent --goal "<task>" [flags]
       celeste agent <task words>
       celeste agent --resume <run-id>
       celeste agent --list-runs
       celeste agent --eval <cases.json>
       celeste agent --benchmark <suite.json> [--benchmark-out <report.json>]

Runs an autonomous agent loop until the task is done.

Task:
  --goal <text>               the task
  --goal-file <path>          read the task from a file
  --resume <run-id>           resume an earlier run
  --list-runs                 list recent runs
  --eval <cases.json>         run evaluation cases
  --benchmark <suite.json>    run a benchmark suite
  --benchmark-out <path>      write the benchmark report as JSON
  --workspace <path>          workspace root (default: the current directory)
  --auto-approve              approve every tool without prompting; needed for
                              tools that ask, since celeste agent cannot prompt
Limits:
  --max-turns N               maximum agent turns
  --max-tool-calls N          maximum tool calls per turn
  --max-no-tool-turns N       maximum consecutive turns without a tool call
  --request-timeout N         model request timeout in seconds
  --tool-timeout N            tool timeout in seconds
  --verify-timeout N          verification command timeout in seconds
Completion:
  --require-complete-marker   require the completion marker (default true)
  --completion-marker <text>  the marker (default "TASK_COMPLETE:")
  --planner                   plan first (default true)
  --plan-max-steps N          maximum steps taken from the plan
  --require-verify            require the verification commands to pass
  --verify-cmd <command>      a verification command (repeatable)
Output:
  --artifact-dir <path>       where run artifact bundles go
  --no-artifacts              write no artifact bundle
  --no-checkpoint             keep no checkpoints for this run
  --verbose                   print each turn (default true)`,
	"init": `Usage: celeste init [--agents]

Creates a starter .grimoire for the current project. A file that already
exists is left as it is.
  --agents  also write AGENTS.md (build and test commands for coding agents)`,
	"grimoire": `Usage: celeste grimoire

Shows the project grimoire for the current directory, all layers merged.`,
	"index": `Usage: celeste index [rebuild|status|reset]

Builds or updates the code graph index for the current directory.
  rebuild  delete the index and build it from scratch
  status   show the index's summary without changing it
  reset    delete the index`,
	"serve": `Usage: celeste [-config <name>] serve [--sse [--port N] [--remote] [--cert <file> --key <file>]]

Starts the MCP server, on stdio by default.
  --sse          use the SSE transport instead of stdio
  --port N       the SSE port (default 8420)
  --remote       bind to 0.0.0.0 for network access (needs --cert and --key)
  --cert <file>  TLS certificate file
  --key <file>   TLS private key file`,
	"costs": `Usage: celeste costs

Shows the cost breakdown of the current session.`,
	"memories": `Usage: celeste memories

Lists the memories saved for the current project.`,
	"remember": `Usage: celeste remember "<text>"

Saves the text as a memory for this project; celeste memories lists them.`,
	"forget": `Usage: celeste forget <memory-name>

Deletes a project memory by name; celeste memories lists them.`,
	"resume": `Usage: celeste resume [<id or name>]

Lists saved chat sessions, or opens the chat UI on the one given.`,
	"plan":   planCLIUsage,
	"revert": revertUsage,
	"mcp": `Usage: celeste mcp list
       celeste mcp trust [--yes] <server>
       celeste mcp untrust <server>
       celeste mcp install [--client <name>] [--dry-run] [--port N]

list shows the MCP servers configured in your home and this directory: each
one's source, transport, enabled and trusted flags, whether a workspace
server is approved, declined or pending, and where it runs
(celeste mcp list --help).

trust approves one of this directory's servers (also one you declined)
after showing its command, args and source file and asking to confirm;
--yes skips the question (needed without a terminal). untrust forgets the
approval or decline, so the chat asks again.

install adds celeste to MCP clients' configs.
  --client <name>  all (the default), claude-desktop, claude-code, cursor,
                   celeste-cli or codex
  --dry-run        print the changes without writing them
  --port N         configure the SSE transport on this port instead of stdio`,
	"hooks": strings.TrimRight(hooksUsage, "\n"),
	"acp": `Usage: celeste [-config <name>] acp

Runs celeste as an Agent Client Protocol agent over stdio (Zed, JetBrains).`,
	"update": `Usage: celeste update [--check]

Installs the latest official release (go install and release builds).
  --check  only report whether a newer release exists`,
	"persona": `Usage: celeste persona verify

Checks that this binary carries the official persona.`,
}

// isHelpFlag reports whether arg asks for help.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "-help"
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
