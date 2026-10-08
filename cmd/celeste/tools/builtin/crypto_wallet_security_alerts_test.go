package builtin

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alertWithKey(i int) SecurityAlert {
	return SecurityAlert{
		ID: generateAlertID(), WalletAddress: testWallet, AlertType: "dust_attack",
		TxHash: fmt.Sprintf("0x%x", i), Key: fmt.Sprintf("eth-mainnet|%d", i),
	}
}

// Aikido 806869535: stored alerts are deduplicated and bounded.
func TestWalletAlertsDeduplicatedAndBounded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	a := alertWithKey(1)
	again := a
	again.ID = generateAlertID()
	added, err := appendAlerts([]SecurityAlert{a, again})
	require.NoError(t, err)
	assert.Len(t, added, 1)
	added, err = appendAlerts([]SecurityAlert{a})
	require.NoError(t, err)
	assert.Empty(t, added)

	batch := make([]SecurityAlert, 0, maxStoredAlerts+5)
	for i := 2; i < maxStoredAlerts+7; i++ {
		batch = append(batch, alertWithKey(i))
	}
	_, err = appendAlerts(batch)
	require.NoError(t, err)
	log, err := loadAlertsLog()
	require.NoError(t, err)
	require.Len(t, log.Alerts, maxStoredAlerts)
	assert.Equal(t, "eth-mainnet|7", log.Alerts[0].Key, "the oldest are dropped first")
	assert.Equal(t, fmt.Sprintf("eth-mainnet|%d", maxStoredAlerts+6), log.Alerts[len(log.Alerts)-1].Key)

	res, err := handleGetSecurityAlerts(map[string]any{})
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Len(t, m["alerts"], maxReturnedAlerts)
	assert.Equal(t, maxStoredAlerts, m["total"])
	assert.Equal(t, true, m["truncated"])
}

// Scanning the same range twice reports and stores each event once.
func TestWalletRescanDoesNotDuplicateAlerts(t *testing.T) {
	f := &fakeAlchemy{transfers: func(dir, key string) ([]any, string) {
		if dir == "to" {
			return []any{dustTransfer("0xfff")}, ""
		}
		return nil, ""
	}}
	useFakeAlchemy(t, f)

	for i, want := range []int{1, 0} {
		require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
			MonitoredWallets: []MonitoredWallet{{Address: testWallet, Network: "eth-mainnet"}},
			LastCheckedBlock: "0x100",
		}))
		res, err := handleCheckWalletSecurity(context.Background(), walletTestLoader{})
		require.NoError(t, err)
		assert.Equal(t, want, res.(map[string]any)["alerts_found"], "scan %d", i)
	}
	log, err := loadAlertsLog()
	require.NoError(t, err)
	assert.Len(t, log.Alerts, 1)
}
