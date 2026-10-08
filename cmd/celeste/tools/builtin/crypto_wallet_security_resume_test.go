package builtin

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pagedTransfers serves the given blocks' transfers to the "to" direction,
// one per page, starting at the request's fromBlock.
func pagedTransfers(blocks []string) func(dir, fromBlock, key string) ([]any, string) {
	return func(dir, fromBlock, key string) ([]any, string) {
		if dir != "to" {
			return nil, ""
		}
		from, _ := parseHexBlock(fromBlock)
		var list []map[string]any
		for i, b := range blocks {
			if n, _ := parseHexBlock(b); n.Cmp(from) < 0 {
				continue
			}
			tr := benignTransfer(fmt.Sprintf("0x%x", 0x1000+i))
			tr["blockNum"] = b
			list = append(list, tr)
		}
		i := 0
		if key != "" {
			i, _ = strconv.Atoi(key)
		}
		if i >= len(list) {
			return nil, ""
		}
		next := ""
		if i+1 < len(list) {
			next = strconv.Itoa(i + 1)
		}
		return []any{list[i]}, next
	}
}

func scanOK(t *testing.T) {
	t.Helper()
	res, err := handleCheckWalletSecurity(context.Background(), walletTestLoader{})
	require.NoError(t, err)
	m, _ := res.(map[string]any)
	require.Equal(t, true, m["success"], m)
}

func checkpointOf(t *testing.T, network string) string {
	t.Helper()
	cfg, err := loadWalletSecurityConfig()
	require.NoError(t, err)
	return cfg.LastCheckedBlocks[network]
}

// A checkpoint means "scanned through"; the next scan starts after it, so a
// block that, together with the next one, fills the page cap is not read
// again and again: consecutive cut-off scans advance strictly.
func TestWalletCutOffScansAdvance(t *testing.T) {
	var blocks []string
	for i := 0; i < 30; i++ {
		blocks = append(blocks, "0x150")
	}
	for i := 0; i < 30; i++ {
		blocks = append(blocks, "0x151")
	}
	f := &fakeAlchemy{transfersAt: pagedTransfers(blocks)}
	useFakeAlchemy(t, f)
	require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
		MonitoredWallets:  []MonitoredWallet{{Address: testWallet, Network: "eth-mainnet"}},
		LastCheckedBlocks: map[string]string{"eth-mainnet": "0x100"},
	}))

	scanOK(t)
	first := checkpointOf(t, "eth-mainnet")
	assert.Equal(t, "0x150", first)

	scanOK(t)
	second := checkpointOf(t, "eth-mainnet")
	a, _ := parseHexBlock(first)
	b, _ := parseHexBlock(second)
	assert.Equal(t, 1, b.Cmp(a), "checkpoint did not advance: %s then %s", first, second)
	assert.Equal(t, "0x200", second)
	assert.Equal(t, []string{"0x101", "0x101", "0x151", "0x151"}, f.fromBlocks)
}

// A network already scanned through the current block is not scanned again.
func TestWalletScanUpToDateNetworkSkipped(t *testing.T) {
	f := &fakeAlchemy{}
	useFakeAlchemy(t, f)
	require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
		MonitoredWallets:  []MonitoredWallet{{Address: testWallet, Network: "eth-mainnet"}},
		LastCheckedBlocks: map[string]string{"eth-mainnet": "0x200"},
	}))

	scanOK(t)
	assert.Empty(t, f.fromBlocks)
	assert.Equal(t, "0x200", checkpointOf(t, "eth-mainnet"))
}

// A legacy single checkpoint was also "scanned through": it is migrated to
// the first wallet's network and the scan starts after it.
func TestWalletLegacyCheckpointMigrated(t *testing.T) {
	f := &fakeAlchemy{}
	useFakeAlchemy(t, f) // legacy file: last_checked_block 0x100, no map

	scanOK(t)
	assert.Equal(t, []string{"0x101", "0x101"}, f.fromBlocks)
	cfg, err := loadWalletSecurityConfig()
	require.NoError(t, err)
	assert.Equal(t, "0x200", cfg.LastCheckedBlocks["eth-mainnet"])
	assert.Empty(t, cfg.LastCheckedBlock, "legacy field is not written back")
}

// The legacy field is read only from an old-format file. A network that has
// never completed a scan does not inherit another chain's checkpoint.
func TestWalletLegacyCheckpointNotAppliedToOtherChain(t *testing.T) {
	f := &fakeAlchemy{}
	useFakeAlchemy(t, f)
	require.NoError(t, saveWalletSecurityConfig(&WalletSecurityConfig{
		MonitoredWallets:  []MonitoredWallet{{Address: testWallet, Network: "base-mainnet"}},
		LastCheckedBlock:  "0x150",
		LastCheckedBlocks: map[string]string{"eth-mainnet": "0x150"},
	}))

	scanOK(t)
	// First scan of base looks back 100 blocks from 0x200.
	assert.Equal(t, []string{"0x19c", "0x19c"}, f.fromBlocks)
	assert.Equal(t, "0x200", checkpointOf(t, "base-mainnet"))
}

// Cut off at the page cap one block past the start: that first block is
// complete, so the scan advances by it rather than failing.
func TestWalletScanCutOffOneBlockPastStart(t *testing.T) {
	var blocks []string
	for i := 0; i < 30; i++ {
		blocks = append(blocks, "0x100")
	}
	for i := 0; i < 30; i++ {
		blocks = append(blocks, "0x101")
	}
	f := &fakeAlchemy{transfersAt: pagedTransfers(blocks)}
	wallet := MonitoredWallet{Address: testWallet, Network: "eth-mainnet"}
	_, scannedTo, err := checkWalletForThreats(context.Background(), f.client(), AlchemyConfig{APIKey: "k"}, wallet, "0x100", "0x200")
	require.NoError(t, err)
	assert.Equal(t, "0x100", scannedTo)
}
