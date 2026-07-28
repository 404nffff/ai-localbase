package util

import (
	"strings"
	"testing"
)

func TestNextIDUsesUniqueProcessIndependentValues(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := NextID("doc")
		if !strings.HasPrefix(id, "doc-") {
			t.Fatalf("expected doc prefix, got %q", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("generated duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}
