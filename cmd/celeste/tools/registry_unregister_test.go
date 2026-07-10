package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "alpha"})
	r.Register(&mockTool{name: "beta"})
	assert.Equal(t, 2, r.Count())

	r.Unregister("alpha")
	assert.Equal(t, 1, r.Count())
	_, ok := r.Get("alpha")
	assert.False(t, ok)

	// Unregistering a missing tool is a no-op, not a panic.
	r.Unregister("does-not-exist")
	assert.Equal(t, 1, r.Count())
}

func TestRegistry_UnregisterByPrefix(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "srv__a"})
	r.Register(&mockTool{name: "srv__b"})
	r.Register(&mockTool{name: "keep"})

	n := r.UnregisterByPrefix("srv__")
	assert.Equal(t, 2, n)
	assert.Equal(t, 1, r.Count())
	_, ok := r.Get("keep")
	assert.True(t, ok)
}
