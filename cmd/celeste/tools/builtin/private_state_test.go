package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Aikido 806869849: notes, reminders, QR codes and wallet state under
// ~/.celeste are owner-only, also over files an older version left 0644.
func TestAccountPrivateStateIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.json")
	if err := os.WriteFile(notes, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(notes, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if res, err := NewNoteSaveTool().Execute(ctx, map[string]any{"title": "t", "content": "c"}, nil); err != nil || res.Error {
		t.Fatalf("save_note: %v %+v", err, res)
	}
	if res, err := NewReminderSetTool().Execute(ctx, map[string]any{"message": "m", "time": "2099-01-02 03:04"}, nil); err != nil || res.Error {
		t.Fatalf("set_reminder: %v %+v", err, res)
	}
	if res, err := NewQRCodeTool().Execute(ctx, map[string]any{"text": "hello"}, nil); err != nil || res.Error {
		t.Fatalf("generate_qr_code: %v %+v", err, res)
	}
	if err := saveWalletSecurityConfig(&WalletSecurityConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := saveAlertsLog(&AlertsLog{}); err != nil {
		t.Fatal(err)
	}
	want := map[string]os.FileMode{
		dir:                                  0o700,
		notes:                                0o600,
		filepath.Join(dir, "reminders.json"): 0o600,
		filepath.Join(dir, "qr_codes"):       0o700,
		filepath.Join(dir, "wallet_security.json"): 0o600,
		filepath.Join(dir, "wallet_alerts.json"):   0o600,
	}
	qrs, _ := filepath.Glob(filepath.Join(dir, "qr_codes", "*.png"))
	if len(qrs) != 1 {
		t.Fatalf("qr files = %v", qrs)
	}
	want[qrs[0]] = 0o600
	for p, mode := range want {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s mode = %v, want %v", filepath.Base(p), fi.Mode().Perm(), mode)
		}
	}
}
