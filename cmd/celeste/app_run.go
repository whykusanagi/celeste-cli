package main

import (
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

func main() {
	os.Exit(run(os.Args[1:], defaultCommandRunner{}, os.Stdout, os.Stderr))
}

func run(args []string, runner commandRunner, stdout, stderr io.Writer) int {
	resetGlobalFlags()
	args = extractGlobalFlags(args)

	if len(args) < 1 {
		runner.PrintUsage()
		if runner.HasDefaultConfig() {
			fmt.Fprintln(stdout, "\n💡 Tip: You have a default configuration. Maybe you meant `celeste chat`?")
		}
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
	case "help", "-h", "--help":
		runner.PrintUsage()
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "Celeste CLI %s (%s)\n", Version, Build)
	default:
		// Treat unknown command as a message.
		runner.RunSingleMessage(strings.Join(args, " "))
	}

	return 0
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
