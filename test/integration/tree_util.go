//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// operations for compare files (local vs remote)

type TreeEntry struct {
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

// cmp utils

func assertTreeMapsEqual(t *testing.T, want, got map[string]TreeEntry) {
	t.Helper()

	wantKeys := mapKeys(want)
	gotKeys := mapKeys(got)

	assert.Equal(t, wantKeys, gotKeys, "tree paths differ")

	for _, k := range wantKeys {
		assert.Equal(t, want[k], got[k], "tree entry mismatch for %s", k)
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// file utils

func fileEntryFromContent(path, content string) TreeEntry {
	sum := sha256.Sum256([]byte(content))
	return TreeEntry{
		Path:   path,
		IsDir:  false,
		Size:   int64(len(content)),
		SHA256: hex.EncodeToString(sum[:]),
	}
}
