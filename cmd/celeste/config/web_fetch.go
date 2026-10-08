package config

import (
	"os"
	"sync/atomic"
)

// WebFetchAllowPrivateEnv set to 1 lets web_fetch reach non-public addresses.
const WebFetchAllowPrivateEnv = "CELESTE_WEB_FETCH_ALLOW_PRIVATE"

// sessionWebFetchAllowPrivate is the opt-in of the config the session runs
// with, recorded by LoadNamedWithEnv.
var sessionWebFetchAllowPrivate atomic.Bool

// WebFetchPrivateAllowed reports whether web_fetch may reach loopback,
// private-network, link-local and other non-public addresses:
// CELESTE_WEB_FETCH_ALLOW_PRIVATE=1, or "web_fetch_allow_private": true in
// the config this session was started with.
func WebFetchPrivateAllowed() bool {
	return os.Getenv(WebFetchAllowPrivateEnv) == "1" || sessionWebFetchAllowPrivate.Load()
}
