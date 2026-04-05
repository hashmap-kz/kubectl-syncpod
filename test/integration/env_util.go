//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type testEnv struct {
	t         *testing.T
	ctx       context.Context
	Namespace string
	BinPath   string
}

func newTestEnv(t *testing.T, ctx context.Context, ns string) *testEnv {
	t.Helper()

	env := &testEnv{
		t:         t,
		ctx:       ctx,
		Namespace: ns,
		BinPath:   mustFindBinary(t),
	}

	nsCreate, err := runCmd("kubectl", "create", "ns", ns, "--dry-run=client", "-oyaml")
	require.NoError(t, err)
	_, err = runCmdWithStdin(nsCreate, "kubectl", "apply", "-f", "-")
	require.NoError(t, err)
	return env
}

func newExistingTestEnv(t *testing.T, ctx context.Context, ns string) *testEnv {
	t.Helper()

	return &testEnv{
		t:         t,
		ctx:       ctx,
		Namespace: ns,
		BinPath:   mustFindBinary(t),
	}
}

func (e *testEnv) Cleanup() {
	e.t.Helper()
}

func mustFindBinary(t *testing.T) string {
	t.Helper()

	if p := os.Getenv("KUBECTL_SYNCPOD_BIN"); p != "" {
		return p
	}

	p := filepath.Join(".", "bin", "kubectl-syncpod")
	if _, err := os.Stat(p); err == nil {
		return p
	}

	t.Fatalf("set KUBECTL_SYNCPOD_BIN or place binary at %s", p)
	return ""
}
