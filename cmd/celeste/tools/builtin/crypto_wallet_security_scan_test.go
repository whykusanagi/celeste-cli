package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWallet = "0x52908400098527886E0F7030069857D2E4169EE7"

// fakeAlchemy answers Alchemy JSON-RPC calls in-process (no network).
type fakeAlchemy struct {
	mu sync.Mutex
	// transfers returns one page for a direction ("from"/"to") and pageKey.
	transfers func(direction, pageKey string) (page []any, next string)
	// fail makes the named method return a transport error.
	fail map[string]bool
	// failNetwork makes every call to that network fail.
	failNetwork string
	calls       map[string]int
}

func (f *fakeAlchemy) RoundTrip(req *http.Request) (*http.Response, error) {
	var body struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	raw, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[body.Method]++
	failing := f.fail[body.Method] || (f.failNetwork != "" && strings.HasPrefix(req.URL.Host, f.failNetwork+"."))
	f.mu.Unlock()
	if failing {
		return nil, errors.New("simulated outage")
	}

	var result any
	switch body.Method {
	case "eth_blockNumber":
		result = "0x200"
	case "eth_getBalance":
		result = "0xde0b6b3a7640000" // 1 ETH
	case "eth_getLogs":
		result = []any{}
	case "alchemy_getAssetTransfers":
		p, _ := body.Params[0].(map[string]any)
		dir := "to"
		if _, ok := p["fromAddress"]; ok {
			dir = "from"
		}
		key, _ := p["pageKey"].(string)
		page, next := []any{}, ""
		if f.transfers != nil {
			page, next = f.transfers(dir, key)
		}
		r := map[string]any{"transfers": page}
		if next != "" {
			r["pageKey"] = next
		}
		result = r
	default:
		result = nil
	}
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(out)), Header: http.Header{}, Request: req}, nil
}

func (f *fakeAlchemy) client() *http.Client { return &http.Client{Transport: f} }

func dustTransfer(hash string) map[string]any {
	return map[string]any{
		"category": "external", "blockNum": "0x150", "from": "0x1111111111111111111111111111111111111111",
		"to": "0x52908400098527886e0f7030069857d2e4169ee7", "value": 0.00001, "asset": "ETH",
		"hash": hash, "uniqueId": hash + ":external",
	}
}

func benignTransfer(hash string) map[string]any {
	return map[string]any{
		"category": "external", "blockNum": "0x150", "from": "0x1111111111111111111111111111111111111111",
		"to": "0x52908400098527886e0f7030069857d2e4169ee7", "value": 5.0, "asset": "ETH",
		"hash": hash, "uniqueId": hash + ":external",
	}
}

// Aikido 806869642: transfers on continuation pages are scanned too.
func TestWalletScanFollowsTransferPages(t *testing.T) {
	f := &fakeAlchemy{transfers: func(dir, key string) ([]any, string) {
		if dir != "to" {
			return nil, ""
		}
		switch key {
		case "":
			return []any{benignTransfer("0xaaa")}, "page-2"
		case "page-2":
			return []any{benignTransfer("0xbbb")}, "page-3"
		case "page-3":
			return []any{dustTransfer("0xccc")}, ""
		}
		return nil, ""
	}}
	wallet := MonitoredWallet{Address: testWallet, Network: "eth-mainnet"}
	alerts, err := checkWalletForThreats(context.Background(), f.client(), AlchemyConfig{APIKey: "k"}, wallet, "0x100", "0x200")
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	assert.Equal(t, "dust_attack", alerts[0].AlertType)
	assert.Equal(t, "0xccc", alerts[0].TxHash)
}

// A scan whose pagination never finishes is a failed scan, not a short one.
func TestWalletScanFailsOnEndlessPages(t *testing.T) {
	n := 0
	f := &fakeAlchemy{transfers: func(dir, key string) ([]any, string) {
		n++
		return []any{benignTransfer("0xddd")}, fmt.Sprintf("more-%d", n)
	}}
	wallet := MonitoredWallet{Address: testWallet, Network: "eth-mainnet"}
	_, err := checkWalletForThreats(context.Background(), f.client(), AlchemyConfig{APIKey: "k"}, wallet, "0x100", "0x200")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pages")
	assert.Equal(t, maxAssetTransferPages, n)
}

// Alchemy reports addresses in lower case; a checksummed monitored address
// still matches.
func TestWalletDetectorsMatchAddressCaseInsensitively(t *testing.T) {
	tr := parseAssetTransfer(dustTransfer("0xeee"))
	assert.NotNil(t, detectDustAttack(tr, testWallet))
}
