//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	assertNoSyncpodResourcesLeft(t, ns)
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
	assertNoSyncpodResourcesLeft(t, ns)
}

func TestIntegration_DistrolessPVC_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutPerTest)
	defer cancel()

	const (
		ns  = "distroless-syncpod-test"
		pvc = "distroless"
		pod = "distroless"
		mnt = "/data"
	)

	env := newExistingTestEnv(t, ctx, ns)

	waitPodReady(t, ns, pod)

	remoteDir := fmt.Sprintf("syncpod-it/%d", time.Now().UnixNano())
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	writeTestTree(t, srcDir, map[string]string{
		"payload/a.txt":                        "hello from distroless",
		"payload/nested/b.txt":                 "round trip works",
		"payload/empty.txt":                    "",
		"payload/spaced name.txt":              "with spaces",
		"payload/unicode-файл.txt":             "unicode ok",
		"payload/deeper/x/y/z.txt":             "deep",
		"payload/long/content.txt":             strings.Repeat("abc123", 4096),
		"payload/mixed/0123456789.bin-as-text": strings.Repeat("0123456789", 2048),
	})

	_, err := runCmd(env.BinPath,
		"upload",
		"--namespace", ns,
		"--pvc", pvc,
		"--mount-path", mnt,
		"--src", filepath.Join(srcDir, "payload"),
		"--dst", remoteDir,
		"--workers", "2",
	)
	require.NoError(t, err)

	_, err = runCmd(env.BinPath,
		"download",
		"--namespace", ns,
		"--pvc", pvc,
		"--mount-path", mnt,
		"--src", remoteDir,
		"--dst", dstDir,
		"--workers", "2",
	)
	require.NoError(t, err)

	want := buildLocalTreeMap(t, filepath.Join(srcDir, "payload"))
	got := buildLocalTreeMap(t, dstDir)
	assertTreeMapsEqual(t, want, got)
	assertNoSyncpodResourcesLeft(t, ns)

	// The original distroless workload must stay healthy during the helper-pod round trip.
	waitPodReady(t, ns, pod)
}
