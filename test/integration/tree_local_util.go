package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// operations with local files

func buildLocalTreeMap(t *testing.T, root string) map[string]TreeEntry {
	t.Helper()

	m := map[string]TreeEntry{
		".": {Path: ".", IsDir: true},
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)

		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)

		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		if info.IsDir() {
			m[rel] = TreeEntry{
				Path:  rel,
				IsDir: true,
			}
			return nil
		}

		data, err := os.ReadFile(path)
		require.NoError(t, err)

		sum := sha256.Sum256(data)
		m[rel] = TreeEntry{
			Path:   rel,
			IsDir:  false,
			Size:   info.Size(),
			SHA256: hex.EncodeToString(sum[:]),
		}

		return nil
	})
	require.NoError(t, err)

	return m
}

func writeTestTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}
