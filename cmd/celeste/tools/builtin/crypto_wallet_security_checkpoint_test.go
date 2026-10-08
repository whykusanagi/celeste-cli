package builtin

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type walletTestLoader struct{ countingConfigLoader }

func (walletTestLoader) GetAlchemyConfig() (AlchemyConfig, error) {
	return AlchemyConfig{APIKey: "test-key"}, nil
}

// useFakeAlchemy points the wallet monitor at f and its storage at a temp home.
func useFakeAlchemy(t *testing.T, f *fakeAlchemy) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	prev := walletSecurityClient
	walletSecurityClient = func() *http.Client { return f.client() }
	t.Cleanup(func() { walletSecurityClient = prev })
	require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
		MonitoredWallets: []MonitoredWallet{{Address: testWallet, Network: "eth-mainnet"}},
		LastCheckedBlock: "0x100",
	}))
}

// Aikido 806869487: a scan that fails for any wallet leaves the checkpoint
// where it was, so the range is scanned again next time.
func TestWalletCheckpointHeldOnFailedScan(t *testing.T) {
	for _, method := range []string{"alchemy_getAssetTransfers", "eth_getLogs"} {
		t.Run(method, func(t *testing.T) {
			f := &fakeAlchemy{fail: map[string]bool{method: true}}
			useFakeAlchemy(t, f)

			res, err := handleCheckWalletSecurity(context.Background(), walletTestLoader{})
			require.NoError(t, err)
			m, _ := res.(map[string]any)
			assert.Equal(t, false, m["success"], m)

			cfg, err := loadWalletSecurityConfig()
			require.NoError(t, err)
			assert.Equal(t, "0x100", cfg.LastCheckedBlock)
		})
	}
}

func TestWalletCheckpointAdvancesOnCleanScan(t *testing.T) {
	f := &fakeAlchemy{}
	useFakeAlchemy(t, f)

	res, err := handleCheckWalletSecurity(context.Background(), walletTestLoader{})
	require.NoError(t, err)
	m, _ := res.(map[string]any)
	assert.Equal(t, true, m["success"], m)

	cfg, err := loadWalletSecurityConfig()
	require.NoError(t, err)
	assert.Equal(t, "0x200", cfg.LastCheckedBlock)
}

// Checkpoints are per network: a failure on one network holds only its own.
func TestWalletCheckpointPerNetwork(t *testing.T) {
	f := &fakeAlchemy{failNetwork: "polygon-mainnet"}
	useFakeAlchemy(t, f)
	require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
		MonitoredWallets: []MonitoredWallet{
			{Address: testWallet, Network: "eth-mainnet"},
			{Address: testWallet, Network: "polygon-mainnet"},
		},
		LastCheckedBlocks: map[string]string{"eth-mainnet": "0x100", "polygon-mainnet": "0x150"},
	}))

	res, err := handleCheckWalletSecurity(context.Background(), walletTestLoader{})
	require.NoError(t, err)
	m, _ := res.(map[string]any)
	assert.Equal(t, false, m["success"], m)

	cfg, err := loadWalletSecurityConfig()
	require.NoError(t, err)
	assert.Equal(t, "0x200", cfg.LastCheckedBlocks["eth-mainnet"])
	assert.Equal(t, "0x150", cfg.LastCheckedBlocks["polygon-mainnet"])
	assert.Equal(t, "0x200", cfg.LastCheckedBlock)
}
