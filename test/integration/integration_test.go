//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	defaultTimeoutPerTest = 10 * time.Minute
	mountPathInContainer  = "/data"
	statePodName          = "state"
)

// basic tests (upload/download to/from PVC)

func TestIntegration_UploadDirectoryTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutPerTest)
	defer cancel()

	ns := fmt.Sprintf("syncpod-it-%d", time.Now().UnixNano())

	// pv+pvc yaml file
	manifest := renderStatePodManifest(t,
		statePodManifestOpts{
			Namespace: ns,
			Name:      statePodName,
			MountPath: mountPathInContainer,
		},
	)

	// setup env, it'll create ns
	env := newTestEnv(t, ctx, ns)
	defer env.Cleanup()

	// apply manifests
	_, err := runCmdWithStdin(manifest, "kubectl", "apply", "-f", "-")
	require.NoError(t, err)

	// generate files locally
	srcDir := t.TempDir()
	writeTestTree(t, srcDir, map[string]string{
		"base/a.txt":            "hello",
		"base/nested/b.txt":     "world",
		"base/empty.txt":        "",
		"base/spaced name.txt":  "with spaces",
		"base/unicode-файл.txt": "unicode ok",
		"base/deeper/x/y/z.txt": "deep",
	})

	// wait while pod is ready, upload local files to pvc
	waitPodReady(t, ns, statePodName)
	_, err = runCmd(env.BinPath,
		"upload",
		"--namespace", ns,
		"--pvc", statePodName,
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "base"),
		"--dst", "payload",
		"--workers", "2",
	)
	require.NoError(t, err)

	// compare local state with pod content
	got := readRemoteTree(t, ns, statePodName, "/data/payload")
	want := buildLocalTreeMap(t, filepath.Join(srcDir, "base"))

	assertTreeMapsEqual(t, want, got)
	assertNoSyncpodResourcesLeft(t, statePodName)
}

func TestIntegration_DownloadDirectoryTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutPerTest)
	defer cancel()

	ns := fmt.Sprintf("syncpod-it-%d", time.Now().UnixNano())

	// pv+pvc yaml file
	manifest := renderStatePodManifest(t,
		statePodManifestOpts{
			Namespace: ns,
			Name:      statePodName,
			MountPath: mountPathInContainer,
		},
	)

	// setup env, it'll create ns
	env := newTestEnv(t, ctx, ns)
	defer env.Cleanup()

	// apply manifests
	_, err := runCmdWithStdin(manifest, "kubectl", "apply", "-f", "-")
	require.NoError(t, err)

	dstDir := t.TempDir()

	// wait while pod is ready, upload files
	waitPodReady(t, ns, statePodName)
	writeRemoteFiles(t, ns, statePodName, mountPathInContainer, map[string]string{
		"payload/a.txt":            "hello",
		"payload/nested/b.txt":     "world",
		"payload/empty.txt":        "",
		"payload/spaced name.txt":  "with spaces",
		"payload/unicode-файл.txt": "unicode ok",
	})

	// download files from pvc to local
	_, err = runCmd(env.BinPath,
		"download",
		"--namespace", ns,
		"--pvc", statePodName,
		"--mount-path", mountPathInContainer,
		"--src", "payload",
		"--dst", dstDir,
		"--workers", "2",
	)
	require.NoError(t, err)

	// cmp result
	got := buildLocalTreeMap(t, dstDir)
	want := map[string]TreeEntry{
		".":                {Path: ".", IsDir: true},
		"a.txt":            fileEntryFromContent("a.txt", "hello"),
		"nested":           {Path: "nested", IsDir: true},
		"nested/b.txt":     fileEntryFromContent("nested/b.txt", "world"),
		"empty.txt":        fileEntryFromContent("empty.txt", ""),
		"spaced name.txt":  fileEntryFromContent("spaced name.txt", "with spaces"),
		"unicode-файл.txt": fileEntryFromContent("unicode-файл.txt", "unicode ok"),
	}

	assertTreeMapsEqual(t, want, got)
	assertNoSyncpodResourcesLeft(t, statePodName)
}
