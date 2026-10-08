package builtin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/textutil"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// maxFetchBytes is the maximum output size for web_fetch results (32KB).
const maxFetchBytes = 32 * 1024

// WebFetchTool fetches a URL and converts the HTML content to markdown.
//
// Only public addresses are fetched: the address a connection actually goes
// to is checked when it is dialed (every redirect hop included), so a host
// name that resolves, or later re-resolves, to a loopback, private-network,
// link-local, metadata, documentation or other reserved address is refused. "web_fetch_allow_private": true
// in the config, or CELESTE_WEB_FETCH_ALLOW_PRIVATE=1, lifts that for local
// docs servers.
type WebFetchTool struct {
	BaseTool
	// allowPrivate reports the opt-in; nil is config.WebFetchPrivateAllowed.
	allowPrivate func() bool
	// dialAllowed decides one dial; nil is dialAllowedDefault. Tests swap it.
	dialAllowed func(ap netip.AddrPort, allowPrivate bool) bool
}

// webFetchAllowPrivateEnv set to 1 lets web_fetch reach non-public addresses.
const webFetchAllowPrivateEnv = config.WebFetchAllowPrivateEnv

// webFetchMaxRedirects bounds the redirect chain.
const webFetchMaxRedirects = 10

// errNonPublicAddress is what the dial guard returns for a refused address.
var errNonPublicAddress = errors.New("destination is not a public address")

// nonPublicPrefixes are the ranges netip's predicates do not cover:
// "this network", CGNAT, IETF protocol assignments, the documentation
// ranges, the retired 6to4 relay anycast, benchmarking, the reserved 240/4
// block (broadcast included), the IPv6 discard-only, IETF protocol
// assignment (Teredo, benchmarking, ORCHID and the rest of 2001::/23),
// documentation and SRv6 SID blocks, deprecated site-local IPv6, the
// deprecated IPv4-compatible ::/96 (:: and ::1 are caught before), and the
// local-use NAT64 prefix 64:ff9b:1::/48 (RFC 8215): a network chooses its
// own layout under it, so the IPv4 address it carries cannot be read and
// the whole prefix is refused.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
)

// isPublicAddr reports whether addr is a globally routable unicast address.
// IPv4 embedded in IPv6 (mapped, well-known NAT64, 6to4) is judged by the
// IPv4 part. A network-specific NAT64 prefix outside 64:ff9b::/96 and
// 64:ff9b:1::/48 looks like any other global address and cannot be told
// apart here: a network that uses one filters at its translator.
func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.Is6() {
		b := addr.As16()
		if nat64Prefix.Contains(addr) {
			return isPublicAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		}
		if sixToFour.Contains(addr) {
			return isPublicAddr(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
		}
	}
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

func dialAllowedDefault(ap netip.AddrPort, allowPrivate bool) bool {
	return allowPrivate || isPublicAddr(ap.Addr())
}

// client builds a per-call HTTP client whose dialer refuses non-public
// addresses. The opt-in is read once per call, when a dial needs it. No
// proxy is used: through a proxy the dialed address would be the proxy's.
func (t *WebFetchTool) client() *http.Client {
	allowedFn := t.dialAllowed
	if allowedFn == nil {
		allowedFn = dialAllowedDefault
	}
	optIn := t.allowPrivate
	if optIn == nil {
		optIn = config.WebFetchPrivateAllowed
	}
	optInOnce := sync.OnceValue(optIn)

	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", errNonPublicAddress, address)
			}
			if allowedFn(ap, false) || allowedFn(ap, optInOnce()) {
				return nil
			}
			return fmt.Errorf("%w: %s", errNonPublicAddress, ap.Addr())
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webFetchMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", webFetchMaxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
}

// NewWebFetchTool creates a WebFetchTool.
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		BaseTool: BaseTool{
			ToolName:        "web_fetch",
			ToolDescription: "Fetch a URL and convert its HTML content to markdown. Optionally provide a prompt to guide extraction. Output is capped at 32KB.",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to fetch.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional extraction guidance to prepend to the output.",
					},
				},
				"required": []string{"url"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"url"},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	rawURL := getStringArg(input, "url", "")
	if rawURL == "" {
		return resultFromMap(formatErrorResponse("validation_error", "url is required", "", nil))
	}

	prompt := getStringArg(input, "prompt", "")

	// Validate URL scheme
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	client := t.client()
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to create request", err.Error(), nil))
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CelesteCLI/1.8)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errNonPublicAddress) {
			return resultFromMap(formatErrorResponse("blocked_destination",
				"Refused: the URL (or a redirect) leads to a destination that is not a public address",
				"web_fetch only reaches public addresses. For a local docs server set \"web_fetch_allow_private\": true in the config or CELESTE_WEB_FETCH_ALLOW_PRIVATE=1.",
				map[string]any{"url": rawURL}))
		}
		return resultFromMap(formatErrorResponse("network_error", "Failed to fetch URL", err.Error(), nil))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("URL returned status %d", resp.StatusCode),
			"The URL may be unavailable or require authentication.",
			map[string]any{"url": rawURL, "status_code": resp.StatusCode},
		))
	}

	// Read body (limit to 1MB to avoid OOM on huge pages)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resultFromMap(formatErrorResponse("network_error", "Failed to read response body", err.Error(), nil))
	}

	// Convert HTML to markdown
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(string(body))
	if err != nil {
		// Fall back to raw text if conversion fails
		markdown = string(body)
	}

	// Prepend extraction prompt if provided
	if prompt != "" {
		markdown = fmt.Sprintf("## Extraction Guidance\n%s\n\n---\n\n%s", prompt, markdown)
	}

	markdown = capFetched(markdown)

	return resultFromMap(map[string]any{
		"url":     rawURL,
		"content": markdown,
	})
}

// capFetched cuts fetched content to maxFetchBytes on a character boundary
// and says so.
func capFetched(markdown string) string {
	if len(markdown) <= maxFetchBytes {
		return markdown
	}
	return textutil.CutBytes(markdown, maxFetchBytes) + "\n\n[Content truncated at 32KB]"
}
