package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/acp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// runACPCommand is `celeste [-config name] acp`: an Agent Client Protocol
// agent on stdin/stdout for editors such as Zed and JetBrains (2.0 W4).
func runACPCommand(args []string) {
	fs := flag.NewFlagSet("acp", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: celeste [-config <name>] acp")
		fmt.Fprintln(os.Stderr, "Runs celeste as an Agent Client Protocol agent over stdio (Zed, JetBrains).")
	}
	_ = fs.Parse(args)

	// A go install build upgrades in the background for the next launch;
	// stdout stays the protocol's (W5 ruling 28).
	newUpgradeHook().background()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	load := func() (*config.Config, error) { return config.LoadNamedWithEnv(configName) }
	if err := runACP(ctx, os.Stdin, os.Stdout, os.Stderr, load); err != nil {
		fmt.Fprintf(os.Stderr, "celeste acp: %v\n", err)
		os.Exit(1)
	}
}

// runACP serves ACP on in/out until the editor closes in or ctx ends. Out
// carries only the protocol: while it runs, os.Stdout is stderr (when
// stderr is a file), so a stray write from a dependency cannot corrupt the
// stream, and the agent logs to the log file.
func runACP(ctx context.Context, in io.Reader, out io.Writer, stderr io.Writer, loadConfig func() (*config.Config, error)) error {
	if f, ok := stderr.(*os.File); ok {
		prev := os.Stdout
		os.Stdout = f
		defer func() { os.Stdout = prev }()
	}
	if err := tui.InitLogging(); err != nil {
		fmt.Fprintf(stderr, "celeste acp: logging is off: %v\n", err)
	}
	defer tui.CloseLogging()
	logf := func(format string, args ...any) { tui.LogInfo(fmt.Sprintf(format, args...)) }

	prevMigrationWarn := config.MigrationWarn
	config.MigrationWarn = func(msg string) { logf("celeste: %s", msg) }
	defer func() { config.MigrationWarn = prevMigrationWarn }()
	if n := prompts.PersonaNotice(); n != "" {
		logf("[persona] %s", n)
	}

	home, _ := os.UserHomeDir()
	agent := acp.NewAgent(acp.Deps{
		Config:   loadConfig,
		Sessions: config.NewSessionManager(),
		Home:     home,
		Logf:     logf,
	})
	conn := acp.NewConn(in, out, agent)
	conn.Logf = logf
	agent.Attach(conn)
	logf("acp: serving")
	err := conn.Serve(ctx)
	agent.Close()
	logf("acp: stopped")
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}
