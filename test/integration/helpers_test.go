//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEnv struct {
	t         *testing.T
	ctx       context.Context
	Namespace string
	BinPath   string
}

type TreeEntry struct {
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

func newTestEnv(t *testing.T, ctx context.Context) *testEnv {
	t.Helper()

	ns := fmt.Sprintf("syncpod-it-%d", time.Now().UnixNano())
	env := &testEnv{
		t:         t,
		ctx:       ctx,
		Namespace: ns,
		BinPath:   mustFindBinary(t),
	}

	env.kubectl("create", "namespace", ns)
	return env
}

func (e *testEnv) Cleanup() {
	e.t.Helper()
	_ = e.kubectlErr("delete", "namespace", e.Namespace, "--wait=false")
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

func (e *testEnv) kubectl(args ...string) string {
	e.t.Helper()
	out, err := e.kubectlCombined(args...)
	require.NoError(e.t, err, "kubectl failed: %s", out)
	return out
}

func (e *testEnv) kubectlErr(args ...string) error {
	e.t.Helper()
	_, err := e.kubectlCombined(args...)
	return err
}

func (e *testEnv) kubectlCombined(args ...string) (string, error) {
	e.t.Helper()

	cmd := exec.CommandContext(e.ctx, "kubectl", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func (e *testEnv) kubectlApply(manifest string) {
	e.t.Helper()

	cmd := exec.CommandContext(e.ctx, "kubectl", "apply", "-f", "-")
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(manifest)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	require.NoError(e.t, err, "kubectl apply failed:\n%s", buf.String())
}

func (e *testEnv) pluginCmd(args ...string) *exec.Cmd {
	e.t.Helper()
	cmd := exec.CommandContext(e.ctx, e.BinPath, args...)
	cmd.Env = os.Environ()
	return cmd
}

func (e *testEnv) RunSyncpod(args ...string) string {
	e.t.Helper()

	cmd := e.pluginCmd(args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	require.NoError(e.t, err, "syncpod failed:\n%s", buf.String())

	return buf.String()
}

func (e *testEnv) RunSyncpodErr(args ...string) error {
	e.t.Helper()

	cmd := e.pluginCmd(args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		e.t.Logf("syncpod output:\n%s", buf.String())
	}
	return err
}

func (e *testEnv) CreatePVC(name, size string) {
	e.t.Helper()

	manifest := fmt.Sprintf(`
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: %s
`, name, e.Namespace, size)

	e.kubectlApply(manifest)
}

// CreatePVCAndConsumer is the correct flow for storage classes using
// WaitForFirstConsumer. It creates the PVC, then creates a pod that mounts it,
// then waits for both PVC Bound and pod Ready.
func (e *testEnv) CreatePVCAndConsumer(name, size string) {
	e.t.Helper()

	e.CreatePVC(name, size)
	e.ensureVerifierPod(name)
	e.WaitPVCBound(name)
	e.waitPodReady("verifier")
}

func (e *testEnv) WaitPVCBound(name string) {
	e.t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		out, err := e.kubectlCombined(
			"-n", e.Namespace,
			"get", "pvc", name,
			"-o", "jsonpath={.status.phase}",
		)
		if err == nil && strings.TrimSpace(out) == "Bound" {
			return
		}
		time.Sleep(2 * time.Second)
	}

	e.dumpPVCAndPodDebug(name, "verifier")
	e.t.Fatalf("PVC %s was not Bound in time", name)
}

func (e *testEnv) ensureVerifierPod(pvc string) string {
	e.t.Helper()

	name := "verifier"

	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: syncpod-it-verifier
spec:
  restartPolicy: Never
  containers:
    - name: verifier
      image: python:3.12-alpine
      command: ["sh", "-c", "sleep 3600"]
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, name, e.Namespace, pvc)

	_ = e.kubectlErr("-n", e.Namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=false")
	e.kubectlApply(manifest)
	return name
}

func (e *testEnv) waitPodReady(name string) {
	e.t.Helper()

	_, err := e.kubectlCombined(
		"-n", e.Namespace,
		"wait",
		"--for=condition=Ready",
		"pod/"+name,
		"--timeout=120s",
	)
	if err == nil {
		return
	}

	e.dumpPVCAndPodDebug("data", name)
	require.NoError(e.t, err)
}

func (e *testEnv) dumpPVCAndPodDebug(pvcName, podName string) {
	e.t.Helper()

	e.t.Log("==== kubectl get pvc ====")
	out, _ := e.kubectlCombined("-n", e.Namespace, "get", "pvc", "-o", "wide")
	e.t.Log(out)

	if pvcName != "" {
		e.t.Log("==== kubectl describe pvc ====")
		out, _ = e.kubectlCombined("-n", e.Namespace, "describe", "pvc", pvcName)
		e.t.Log(out)
	}

	e.t.Log("==== kubectl get pods ====")
	out, _ = e.kubectlCombined("-n", e.Namespace, "get", "pods", "-o", "wide")
	e.t.Log(out)

	if podName != "" {
		e.t.Log("==== kubectl describe pod ====")
		out, _ = e.kubectlCombined("-n", e.Namespace, "describe", "pod", podName)
		e.t.Log(out)

		e.t.Log("==== kubectl logs pod ====")
		out, _ = e.kubectlCombined("-n", e.Namespace, "logs", podName)
		e.t.Log(out)
	}

	e.t.Log("==== kubectl get events ====")
	out, _ = e.kubectlCombined("-n", e.Namespace, "get", "events", "--sort-by=.lastTimestamp")
	e.t.Log(out)
}

func (e *testEnv) execInPod(pod string, cmdStr string) string {
	e.t.Helper()

	return e.kubectl(
		"-n", e.Namespace,
		"exec", pod,
		"--",
		"sh", "-lc", cmdStr,
	)
}

func (e *testEnv) WriteRemoteFiles(pvc, mountPath string, files map[string]string) {
	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	payload, _ := json.Marshal(files)
	script := fmt.Sprintf(`
python3 - <<'PY'
import json, os
files = json.loads(%q)
mount = %q
for rel, content in files.items():
    full = os.path.join(mount, rel)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "wb") as f:
        f.write(content.encode("utf-8"))
PY
`, string(payload), mountPath)

	e.execInPod(pod, script)
}

func (e *testEnv) WriteRemoteFiles0(pvc, mountPath string, files map[string]string) {
	e.t.Helper()

	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	var script strings.Builder
	script.WriteString("set -eu\n")
	for rel, content := range files {
		full := filepath.ToSlash(filepath.Join(mountPath, rel))
		dir := filepath.ToSlash(filepath.Dir(full))
		script.WriteString(fmt.Sprintf("mkdir -p %q\n", dir))
		script.WriteString(fmt.Sprintf("cat > %q <<'EOF'\n%s\nEOF\n", full, content))
	}

	e.execInPod(pod, script.String())
}

func (e *testEnv) ReadRemoteTree(pvc, root string) map[string]TreeEntry {
	e.t.Helper()

	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	script := fmt.Sprintf(`
set -eu
ROOT=%q
cd "$ROOT"
python3 - <<'PY'
import hashlib, json, os

root = "."
items = []

for current, dirs, files in os.walk(root):
    dirs.sort()
    files.sort()

    rel_dir = os.path.relpath(current, root)
    if rel_dir == ".":
        items.append({"path": ".", "is_dir": True, "size": 0})
    else:
        items.append({"path": rel_dir, "is_dir": True, "size": 0})

    for f in files:
        p = os.path.join(current, f)
        rel = os.path.relpath(p, root)
        h = hashlib.sha256()
        with open(p, "rb") as fp:
            while True:
                chunk = fp.read(1024 * 1024)
                if not chunk:
                    break
                h.update(chunk)
        items.append({
            "path": rel,
            "is_dir": False,
            "size": os.path.getsize(p),
            "sha256": h.hexdigest(),
        })

print(json.dumps(items))
PY
`, root)

	out := e.execInPod(pod, script)

	var entries []TreeEntry
	require.NoError(e.t, json.Unmarshal([]byte(out), &entries))

	m := make(map[string]TreeEntry, len(entries))
	for _, it := range entries {
		m[it.Path] = it
	}
	return m
}

func (e *testEnv) ReadRemoteFile(pvc, fullPath string) string {
	e.t.Helper()

	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	return e.execInPod(pod, fmt.Sprintf("cat %q", fullPath))
}

func (e *testEnv) ListRemotePaths(pvc, root string) []string {
	e.t.Helper()

	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	out := e.execInPod(pod, fmt.Sprintf(`find %q -print | sort`, root))

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var res []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res
}

func (e *testEnv) StatRemoteOwner(pvc, fullPath string) string {
	e.t.Helper()

	pod := e.ensureVerifierPod(pvc)
	e.waitPodReady(pod)

	out := e.execInPod(pod, fmt.Sprintf(`stat -c '%%u:%%g' %q`, fullPath))
	return strings.TrimSpace(out)
}

func (e *testEnv) AssertNoSyncpodResourcesLeft() {
	e.t.Helper()

	podsOut := e.kubectl(
		"-n", e.Namespace,
		"get", "pods",
		"-l", "app=kubectl-syncpod",
		"-o", "name",
	)
	svcsOut := e.kubectl(
		"-n", e.Namespace,
		"get", "svc",
		"-l", "app=kubectl-syncpod",
		"-o", "name",
	)

	assert.Empty(e.t, strings.TrimSpace(podsOut), "expected no syncpod pods left")
	assert.Empty(e.t, strings.TrimSpace(svcsOut), "expected no syncpod services left")
}

func writeTestTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

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

func fileEntryFromContent(path, content string) TreeEntry {
	sum := sha256.Sum256([]byte(content))
	return TreeEntry{
		Path:   path,
		IsDir:  false,
		Size:   int64(len(content)),
		SHA256: hex.EncodeToString(sum[:]),
	}
}

func matchedRenameDir(path, original string) bool {
	if path == original {
		return false
	}

	base := filepath.Base(original)
	parent := filepath.ToSlash(filepath.Dir(original))
	path = filepath.ToSlash(path)

	if !strings.HasPrefix(path, parent+"/") {
		return false
	}

	name := strings.TrimPrefix(path, parent+"/")
	return strings.HasPrefix(name, base+"-original-")
}

func randomText(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
