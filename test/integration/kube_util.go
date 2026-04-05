//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queries to kube API

func waitPodReady(t *testing.T, namespace, name string) {
	t.Helper()

	_, err := runCmd("kubectl",
		"-n", namespace,
		"wait",
		"--for=condition=Ready",
		"pod/"+name,
		"--timeout=120s",
	)
	if err == nil {
		return
	}

	require.NoError(t, err)
}

func waitStatefulSetReady(t *testing.T, namespace, name string) {
	t.Helper()

	_, err := runCmd("kubectl",
		"-n", namespace,
		"rollout", "status",
		"statefulset/"+name,
		"--timeout=180s",
	)
	require.NoError(t, err)
}

func execInPod(t *testing.T, ns, pod string, cmdStr string) string {
	out, err := runCmd("kubectl",
		"-n", ns,
		"exec", pod,
		"--",
		"sh", "-lc", cmdStr,
	)
	require.NoError(t, err)
	return out
}

func assertNoSyncpodResourcesLeft(t *testing.T, ns string) {
	t.Helper()

	podsOut, err := runCmd("kubectl",
		"-n", ns,
		"get", "pods",
		"-l", "app.kubernetes.io/name=kubectl-syncpod",
		"-o", "name",
	)
	require.NoError(t, err)

	svcsOut, err := runCmd("kubectl",
		"-n", ns,
		"get", "svc",
		"-l", "app.kubernetes.io/name=kubectl-syncpod",
		"-o", "name",
	)
	require.NoError(t, err)

	assert.Empty(t, strings.TrimSpace(podsOut), "expected no syncpod pods left")
	assert.Empty(t, strings.TrimSpace(svcsOut), "expected no syncpod services left")
}
