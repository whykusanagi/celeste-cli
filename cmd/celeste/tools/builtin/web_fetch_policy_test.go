package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Aikido 806869856: web_fetch refuses non-public destinations by default.
func TestWebFetchRefusesLoopbackByDefault(t *testing.T) {
	t.Setenv(webFetchAllowPrivateEnv, "")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("<p>internal</p>"))
	}))
	defer srv.Close()

	tool := NewWebFetchTool()
	tool.allowPrivate = func() bool { return false }
	for _, target := range []string{srv.URL, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)} {
		result, err := tool.Execute(context.Background(), map[string]any{"url": target}, nil)
		require.NoError(t, err)
		assert.True(t, result.Error, target)
		assert.Contains(t, result.Content, "not a public address", target)
		assert.NotContains(t, result.Content, "internal", target)
	}
	assert.Equal(t, int32(0), hits.Load(), "the server must never be reached")
}

// A redirect is checked again at dial time: the first hop is allowed, the
// second (another address) is not.
func TestWebFetchChecksRedirectTargets(t *testing.T) {
	var hits atomic.Int32
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("<p>internal</p>"))
	}))
	defer inner.Close()
	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL, http.StatusFound)
	}))
	defer outer.Close()

	ou, _ := url.Parse(outer.URL)
	allowed := netip.MustParseAddrPort(ou.Host)
	tool := NewWebFetchTool()
	tool.allowPrivate = func() bool { return false }
	tool.dialAllowed = func(ap netip.AddrPort, allowPrivate bool) bool { return ap == allowed }

	result, err := tool.Execute(context.Background(), map[string]any{"url": outer.URL}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "not a public address")
	assert.Equal(t, int32(0), hits.Load())
}

func TestWebFetchAllowPrivateOptIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<p>local docs</p>"))
	}))
	defer srv.Close()

	t.Setenv(webFetchAllowPrivateEnv, "1")
	tool := NewWebFetchTool()
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error, result.Content)
	assert.Contains(t, result.Content, "local docs")
}

func TestIsPublicAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.9.10", "::1", "0.0.0.0", "::", "0.1.2.3",
		"10.0.0.1", "172.16.5.4", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "fe80::1", "fc00::1", "fd00:ec2::254",
		"100.64.0.1", "100.127.255.254", "192.0.0.170", "198.18.0.1",
		"224.0.0.1", "ff02::1", "255.255.255.255", "240.0.0.1",
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe",
		"2002:7f00:1::", "2002:a9fe:a9fe::", "fec0::1",
		// IPv4-compatible (deprecated) and Teredo, which embeds IPv4 too.
		"::7f00:1", "::a9fe:a9fe", "::808:808", "2001:0:4136:e378:8000:63bf:80ff:fffe",
	}
	for _, s := range blocked {
		assert.False(t, isPublicAddr(netip.MustParseAddr(s)), s)
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "100.128.0.1", "2606:4700:4700::1111", "64:ff9b::808:808", "2002:808:808::"}
	for _, s := range allowed {
		assert.True(t, isPublicAddr(netip.MustParseAddr(s)), s)
	}
}

// Aikido PR #420 review: documentation, benchmarking and other reserved
// ranges are not public, nor is the local-use NAT64 prefix (RFC 8215),
// whose embedded IPv4 address cannot be read without knowing the
// network's own translation layout.
func TestIsPublicAddrRefusesReservedAndLocalNAT64(t *testing.T) {
	for _, s := range []string{
		"192.0.2.1", "198.51.100.7", "203.0.113.200", "192.88.99.1",
		"2001:db8::1", "3fff::1", "100::1", "2001:2::1", "2001:10::1",
		"2001:20::1", "5f00::1",
		"64:ff9b:1::a00:1", "64:ff9b:1:ffff::a9fe:a9fe", "64:ff9b:1::808:808",
		"64:ff9b::c000:201", // NAT64 to a documentation address
	} {
		assert.False(t, isPublicAddr(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{"2001:4860:4860::8888", "2a00:1450:4001::1", "203.0.114.1", "198.51.101.1"} {
		assert.True(t, isPublicAddr(netip.MustParseAddr(s)), s)
	}
}
