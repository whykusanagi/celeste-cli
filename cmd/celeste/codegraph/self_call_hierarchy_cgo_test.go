//go:build cgo

package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// CodeRabbit review of #431: a self call that misses the caller's own class
// resolves through its base classes (transitively, across files) before any
// same-named method of an unrelated class, even one stored first.
func TestBuild_SelfCallPrefersBaseClassMethod(t *testing.T) {
	idx, _ := buildFixture(t, map[string]string{
		"a_other.py": "class Other:\n    def run(self):\n        pass\n    def tick(self):\n        pass\n",
		"b_base.py":  "class Base:\n    def run(self):\n        pass\n",
		"c_mid.py":   "from b_base import Base\n\nclass Mid(Base):\n    def tick(self):\n        pass\n",
		"d_child.py": "from c_mid import Mid\n\nclass Child(Mid):\n    def go(self):\n        self.run()\n        self.tick()\n",
	})
	got := scopedEdgeKeys(t, idx, "d_child.py")
	assert.True(t, got["Child.go -> Base.run"], "inherited run not resolved through the hierarchy: %v", got)
	assert.True(t, got["Child.go -> Mid.tick"], "inherited tick not resolved through the hierarchy: %v", got)
	assert.False(t, got["Child.go -> Other.run"], "self call resolved to an unrelated class: %v", got)
	assert.False(t, got["Child.go -> Other.tick"], "self call resolved to an unrelated class: %v", got)
}
