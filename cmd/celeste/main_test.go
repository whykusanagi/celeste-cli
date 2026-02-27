package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgsValidJSON(t *testing.T) {
	args, err := parseArgs(`{"zip_code":"10001","days":2}`)
	require.NoError(t, err)
	assert.Equal(t, "10001", args["zip_code"])
	assert.Equal(t, float64(2), args["days"])
}

func TestParseArgsInvalidJSON(t *testing.T) {
	args, err := parseArgs(`{"zip_code":}`)
	require.Error(t, err)
	assert.Empty(t, args)
}
