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
