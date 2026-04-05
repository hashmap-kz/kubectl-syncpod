//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

func TestIntegration_DownloadSTS_CreatesManifestAndDownloadsAllPVCs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutPerTest)
	defer cancel()

	ns := fmt.Sprintf("syncpod-sts-it-%d", time.Now().UnixNano())
	env := newTestEnv(t, ctx, ns)
	defer env.Cleanup()

	manifest := renderStatefulSetManifest(t, statefulSetManifestOpts{
		Namespace: ns,
		Name:      "stateful",
		MountPath: mountPathInContainer,
		Replicas:  2,
	})
	_, err := runCmdWithStdin(manifest, "kubectl", "apply", "-f", "-")
	require.NoError(t, err)

	waitStatefulSetReady(t, ns, "stateful")
	writeRemoteFiles(t, ns, "stateful-0", mountPathInContainer, map[string]string{
		"alpha.txt":       "alpha from sts-0",
		"nested/zero.txt": "nested zero",
		"empty.txt":       "",
	})
	writeRemoteFiles(t, ns, "stateful-1", mountPathInContainer, map[string]string{
		"beta.txt":         "beta from sts-1",
		"nested/one.txt":   "nested one",
		"unicode-файл.txt": "unicode ok",
	})

	dstDir := t.TempDir()
	_, err = runCmd(env.BinPath,
		"download-sts", "stateful",
		"--namespace", ns,
		"--dst", dstDir,
		"--volume-workers", "2",
		"--file-workers", "2",
	)
	require.NoError(t, err)

	manifestData := readStatefulSetBackupManifest(t, filepath.Join(dstDir, "manifest.json"))
	assert.Equal(t, ns, manifestData.Namespace)
	assert.Equal(t, "stateful", manifestData.StatefulSet)
	assert.Equal(t, []string{"stateful-0/data", "stateful-1/data"}, manifestEntryLocalPaths(manifestData))

	want0 := map[string]TreeEntry{
		".":               {Path: ".", IsDir: true},
		"alpha.txt":       fileEntryFromContent("alpha.txt", "alpha from sts-0"),
		"nested":          {Path: "nested", IsDir: true},
		"nested/zero.txt": fileEntryFromContent("nested/zero.txt", "nested zero"),
		"empty.txt":       fileEntryFromContent("empty.txt", ""),
	}
	want1 := map[string]TreeEntry{
		".":                {Path: ".", IsDir: true},
		"beta.txt":         fileEntryFromContent("beta.txt", "beta from sts-1"),
		"nested":           {Path: "nested", IsDir: true},
		"nested/one.txt":   fileEntryFromContent("nested/one.txt", "nested one"),
		"unicode-файл.txt": fileEntryFromContent("unicode-файл.txt", "unicode ok"),
	}

	assertTreeMapsEqual(t, want0, buildLocalTreeMap(t, filepath.Join(dstDir, "stateful-0", "data")))
	assertTreeMapsEqual(t, want1, buildLocalTreeMap(t, filepath.Join(dstDir, "stateful-1", "data")))
	assertNoSyncpodResourcesLeft(t, ns)
}

func TestIntegration_UploadSTS_RestoresFromDownloadedBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutPerTest)
	defer cancel()

	ns := fmt.Sprintf("syncpod-sts-it-%d", time.Now().UnixNano())
	env := newTestEnv(t, ctx, ns)
	defer env.Cleanup()

	manifest := renderStatefulSetManifest(t, statefulSetManifestOpts{
		Namespace: ns,
		Name:      "stateful",
		MountPath: mountPathInContainer,
		Replicas:  2,
	})
	_, err := runCmdWithStdin(manifest, "kubectl", "apply", "-f", "-")
	require.NoError(t, err)

	waitStatefulSetReady(t, ns, "stateful")
	writeRemoteFiles(t, ns, "stateful-0", mountPathInContainer, map[string]string{
		"seed/a.txt":      "seed zero",
		"seed/nested.txt": "nested zero",
	})
	writeRemoteFiles(t, ns, "stateful-1", mountPathInContainer, map[string]string{
		"seed/b.txt":      "seed one",
		"seed/nested.txt": "nested one",
	})

	backupDir := t.TempDir()
	_, err = runCmd(env.BinPath,
		"download-sts", "stateful",
		"--namespace", ns,
		"--dst", backupDir,
		"--volume-workers", "2",
		"--file-workers", "2",
	)
	require.NoError(t, err)

	clearRemoteDir(t, ns, "stateful-0", mountPathInContainer)
	clearRemoteDir(t, ns, "stateful-1", mountPathInContainer)

	_, err = runCmd(env.BinPath,
		"upload-sts", "stateful",
		"--namespace", ns,
		"--src", backupDir,
		"--volume-workers", "2",
		"--file-workers", "2",
		"--allow-overwrite",
	)
	require.NoError(t, err)

	want0 := buildLocalTreeMap(t, filepath.Join(backupDir, "stateful-0", "data"))
	want1 := buildLocalTreeMap(t, filepath.Join(backupDir, "stateful-1", "data"))
	got0 := readRemoteTree(t, ns, "stateful-0", mountPathInContainer)
	got1 := readRemoteTree(t, ns, "stateful-1", mountPathInContainer)

	assertTreeMapsEqual(t, want0, got0)
	assertTreeMapsEqual(t, want1, got1)
	assertNoSyncpodResourcesLeft(t, ns)
}

// sts only related helpers

func readStatefulSetBackupManifest(t *testing.T, path string) *kub.StatefulSetBackupManifest {
	t.Helper()

	m, err := kub.ReadStatefulSetBackupManifest(path)
	require.NoError(t, err)
	return m
}

func manifestEntryLocalPaths(m *kub.StatefulSetBackupManifest) []string {
	paths := make([]string, 0, len(m.Entries))
	for _, entry := range m.Entries {
		paths = append(paths, entry.LocalPath)
	}
	sort.Strings(paths)
	return paths
}
