//go:build integration

package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_UploadDirectoryTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	srcDir := t.TempDir()
	writeTestTree(t, srcDir, map[string]string{
		"base/a.txt":            "hello",
		"base/nested/b.txt":     "world",
		"base/empty.txt":        "",
		"base/spaced name.txt":  "with spaces",
		"base/unicode-файл.txt": "unicode ok",
		"base/deeper/x/y/z.txt": "deep",
	})

	env.CreatePVCAndConsumer("data", "1Gi")

	env.RunSyncpod(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "base"),
		"--dst", "payload",
		"--workers", "4",
	)

	got := env.ReadRemoteTree("data", "/data/payload")
	want := buildLocalTreeMap(t, filepath.Join(srcDir, "base"))

	assertTreeMapsEqual(t, want, got)
	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_DownloadDirectoryTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	env.CreatePVCAndConsumer("data", "1Gi")

	env.WriteRemoteFiles("data", "/data", map[string]string{
		"payload/a.txt":            "hello",
		"payload/nested/b.txt":     "world",
		"payload/empty.txt":        "",
		"payload/spaced name.txt":  "with spaces",
		"payload/unicode-файл.txt": "unicode ok",
	})

	dstDir := t.TempDir()

	env.RunSyncpod(
		"download",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", "payload",
		"--dst", dstDir,
		"--workers", "4",
	)

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
	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_RoundTripUploadDownload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	srcDir := t.TempDir()
	restoreDir := t.TempDir()

	writeTestTree(t, srcDir, map[string]string{
		"root/a.txt":              "aaa",
		"root/b/c.txt":            "bbb",
		"root/empty.txt":          "",
		"root/more/deep/file.bin": randomText(32 * 1024),
		"root/spaced name.txt":    "space",
		"root/unicode-файл.txt":   "unicode",
	})

	env.CreatePVCAndConsumer("data", "1Gi")

	env.RunSyncpod(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "root"),
		"--dst", "payload",
		"--workers", "4",
	)

	env.RunSyncpod(
		"download",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", "payload",
		"--dst", restoreDir,
		"--workers", "4",
	)

	want := buildLocalTreeMap(t, filepath.Join(srcDir, "root"))
	got := buildLocalTreeMap(t, restoreDir)

	assertTreeMapsEqual(t, want, got)
	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_RemoteDirRenameBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	srcDir := t.TempDir()
	writeTestTree(t, srcDir, map[string]string{
		"fresh/new.txt": "new-content",
	})

	env.CreatePVCAndConsumer("data", "1Gi")

	env.WriteRemoteFiles("data", "/data", map[string]string{
		"payload/old.txt": "old-content",
	})

	env.RunSyncpod(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "fresh"),
		"--dst", "payload",
		"--workers", "2",
	)

	remoteRoot := env.ListRemotePaths("data", "/data")
	require.Contains(t, remoteRoot, "/data/payload/new.txt")

	var renamedDir string
	for _, p := range remoteRoot {
		if matchedRenameDir(p, "/data/payload") {
			renamedDir = p
			break
		}
	}
	require.NotEmpty(t, renamedDir, "expected old payload dir to be renamed")

	oldTree := env.ReadRemoteTree("data", renamedDir)
	require.Contains(t, oldTree, "old.txt")
	assert.Equal(t, "old-content", env.ReadRemoteFile("data", renamedDir+"/old.txt"))

	newTree := env.ReadRemoteTree("data", "/data/payload")
	require.Contains(t, newTree, "new.txt")
	assert.Equal(t, "new-content", env.ReadRemoteFile("data", "/data/payload/new.txt"))

	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_UploadWithOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	srcDir := t.TempDir()
	writeTestTree(t, srcDir, map[string]string{
		"root/a.txt": "hello",
	})

	env.CreatePVCAndConsumer("data", "1Gi")

	env.RunSyncpod(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "data",
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "root"),
		"--dst", "payload",
		"--owner", "1000:1000",
	)

	uidGid := env.StatRemoteOwner("data", "/data/payload/a.txt")
	assert.Equal(t, "1000:1000", uidGid)

	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_FailureStillCleansResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	env := newTestEnv(t, ctx)
	defer env.Cleanup()

	srcDir := t.TempDir()
	writeTestTree(t, srcDir, map[string]string{
		"root/a.txt": "hello",
	})

	err := env.RunSyncpodErr(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "does-not-exist",
		"--mount-path", "/data",
		"--src", filepath.Join(srcDir, "root"),
		"--dst", "payload",
	)

	require.Error(t, err)
	env.AssertNoSyncpodResourcesLeft()
}

func TestIntegration_DistrolessAlreadyDeployed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	env := &testEnv{
		t:         t,
		ctx:       ctx,
		Namespace: "pgrwl-test",
		BinPath:   mustFindBinary(t),
	}

	assertKubectlObjectExists(t, env, "pvc", "distroless-data")
	waitDeploymentReady(t, env, "distroless")

	srcDir := t.TempDir()
	restoreDir := t.TempDir()

	writeTestTree(t, srcDir, map[string]string{
		"payload/a.txt":            "hello",
		"payload/nested/b.txt":     "world",
		"payload/empty.txt":        "",
		"payload/spaced name.txt":  "with spaces",
		"payload/unicode-файл.txt": "unicode ok",
	})

	want := buildLocalTreeMap(t, filepath.Join(srcDir, "payload"))

	remoteDst := "syncpod-distroless-test-" + time.Now().Format("20060102-150405")

	env.RunSyncpod(
		"upload",
		"--namespace", env.Namespace,
		"--pvc", "distroless-data",
		"--mount-path", "/tmp",
		"--src", filepath.Join(srcDir, "payload"),
		"--dst", remoteDst,
		"--workers", "4",
	)

	env.RunSyncpod(
		"download",
		"--namespace", env.Namespace,
		"--pvc", "distroless-data",
		"--mount-path", "/tmp",
		"--src", remoteDst,
		"--dst", restoreDir,
		"--workers", "4",
	)

	gotLocal := buildLocalTreeMap(t, restoreDir)
	assertTreeMapsEqual(t, want, gotLocal)

	waitDeploymentReady(t, env, "distroless")
}
