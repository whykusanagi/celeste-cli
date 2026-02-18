// Package monitor provides background wallet security monitoring daemon
package monitor

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
)

// Daemon manages the background wallet monitoring process
type Daemon struct {
	configLoader *config.ConfigLoader
	pidFile      string
	ticker       *time.Ticker
	stopChan     chan bool
}

// NewDaemon creates a new monitoring daemon
func NewDaemon(configLoader *config.ConfigLoader) *Daemon {
	homeDir, _ := os.UserHomeDir()
	pidFile := filepath.Join(homeDir, ".celeste", "wallet_monitor.pid")

	return &Daemon{
		configLoader: configLoader,
		pidFile:      pidFile,
		stopChan:     make(chan bool),
	}
}

// Start starts the monitoring daemon in the background
func (d *Daemon) Start() error {
	// Check if already running
	if d.IsRunning() {
		pid, _ := d.GetPID()
		return fmt.Errorf("daemon already running with PID %d", pid)
	}

	// Get poll interval from config
	wsConfig, err := d.configLoader.GetWalletSecurityConfig()
	if err != nil {
		return fmt.Errorf("failed to load wallet security config: %w", err)
	}

	pollInterval := time.Duration(wsConfig.PollInterval) * time.Second
	if pollInterval == 0 {
		pollInterval = 5 * time.Minute // Default: 5 minutes
	}

	// Fork process to background
	if os.Getenv("CELESTE_DAEMON_CHILD") != "1" {
		// Parent process - fork and exit
		cmd := os.Args[0]
		args := append([]string{"wallet-monitor", "run"}, os.Args[2:]...)

		env := append(os.Environ(), "CELESTE_DAEMON_CHILD=1")

		attr := &os.ProcAttr{
			Env:   env,
			Files: []*os.File{nil, nil, nil}, // Detach stdio
		}

		process, err := os.StartProcess(cmd, args, attr)
		if err != nil {
			return fmt.Errorf("failed to fork daemon: %w", err)
		}

		// Save PID
		if err := d.savePID(process.Pid); err != nil {
			_ = process.Kill() // Best effort to clean up
			return fmt.Errorf("failed to save PID: %w", err)
		}

		fmt.Printf("✓ Wallet monitoring daemon started (PID: %d)\n", process.Pid)
		fmt.Printf("  Poll interval: %s\n", pollInterval)
		fmt.Printf("  Check status: celeste wallet-monitor status\n")
		fmt.Printf("  Stop daemon: celeste wallet-monitor stop\n")

		return nil
	}

	// Child process - run as daemon
	return d.run(pollInterval)
}

// run is the main daemon loop (runs in background)
func (d *Daemon) run(pollInterval time.Duration) error {
	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create ticker
	d.ticker = time.NewTicker(pollInterval)
	defer d.ticker.Stop()

	// Run initial check immediately
	d.checkWallets()

	// Main loop
	for {
		select {
		case <-d.ticker.C:
			// Periodic check
			d.checkWallets()

		case <-sigChan:
			// Graceful shutdown
			fmt.Println("Stopping wallet monitoring daemon...")
			d.cleanup()
			return nil

		case <-d.stopChan:
			// Stop requested
			d.cleanup()
			return nil
		}
	}
}

// checkWallets performs wallet security check
func (d *Daemon) checkWallets() {
	// Call wallet security skill
	result, err := skills.WalletSecurityHandler(map[string]interface{}{
		"operation": "check_wallet_security",
	}, d.configLoader)

	if err != nil {
		fmt.Printf("[%s] Error checking wallets: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}

	// Check for alerts
	if resultMap, ok := result.(map[string]interface{}); ok {
		alertsFound, _ := resultMap["alerts_found"].(int)

		if alertsFound > 0 {
			fmt.Printf("[%s] ⚠️  %d security alert(s) detected!\n", time.Now().Format(time.RFC3339), alertsFound)
			fmt.Println("   Run: celeste skill wallet_security --operation get_security_alerts")
		} else {
			fmt.Printf("[%s] ✓ No threats detected\n", time.Now().Format(time.RFC3339))
		}
	}
}

// Stop stops the running daemon
func (d *Daemon) Stop() error {
	pid, err := d.GetPID()
	if err != nil {
		return fmt.Errorf("daemon is not running")
	}

	// Find process
	process, err := os.FindProcess(pid)
	if err != nil {
		d.cleanup()
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead, try to cleanup
		d.cleanup()
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	// Wait for process to exit (up to 5 seconds)
	for i := 0; i < 50; i++ {
		if !d.IsRunning() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	d.cleanup()
	fmt.Printf("✓ Wallet monitoring daemon stopped (PID: %d)\n", pid)

	return nil
}

// Status returns the current daemon status
func (d *Daemon) Status() (string, error) {
	if !d.IsRunning() {
		return "stopped", nil
	}

	pid, _ := d.GetPID()

	// Get config to show poll interval
	wsConfig, _ := d.configLoader.GetWalletSecurityConfig()
	pollInterval := time.Duration(wsConfig.PollInterval) * time.Second
	if pollInterval == 0 {
		pollInterval = 5 * time.Minute
	}

	return fmt.Sprintf("running (PID: %d, interval: %s)", pid, pollInterval), nil
}

// IsRunning checks if the daemon is currently running
func (d *Daemon) IsRunning() bool {
	pid, err := d.GetPID()
	if err != nil {
		return false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to test if process is alive
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// GetPID reads the PID from the PID file
func (d *Daemon) GetPID() (int, error) {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, err
	}

	return pid, nil
}

// savePID writes the PID to the PID file
func (d *Daemon) savePID(pid int) error {
	// Ensure directory exists
	dir := filepath.Dir(d.pidFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write PID
	return os.WriteFile(d.pidFile, []byte(strconv.Itoa(pid)), 0644)
}

// cleanup removes the PID file
func (d *Daemon) cleanup() {
	os.Remove(d.pidFile)
}
