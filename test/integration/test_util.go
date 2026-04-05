//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"text/template"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exec utils

// Without stdin
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out.String(), nil
}

// With stdin
func runCmdWithStdin(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)

	cmd.Stdin = strings.NewReader(stdin)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out.String(), nil
}

// func execExample() {
// 	// Without stdin
// 	out, err := runCmd("echo", "hello world")
// 	fmt.Printf("output: %q, err: %v\n", out, err)
//
// 	// With stdin - pipe text through `cat`
// 	out, err = runCmdWithStdin("hello from stdin\n", "cat")
// 	fmt.Printf("output: %q, err: %v\n", out, err)
//
// 	// With stdin - count words via `wc -w`
// 	out, err = runCmdWithStdin("one two three", "wc", "-w")
// 	fmt.Printf("word count: %q, err: %v\n", strings.TrimSpace(out), err)
// }

// test env

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

func writeTestTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

// manifests

type statePodManifestOpts struct {
	Namespace string
	Name      string
	MountPath string
}

type statefulSetManifestOpts struct {
	Namespace string
	Name      string
	MountPath string
	Replicas  int
}

var statePodManifestTmpl = template.Must(template.New("pod").Parse(`
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi

---
apiVersion: v1
kind: Pod
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  restartPolicy: Never
  containers:
    - name: verifier
      image: python:3.12-alpine
      imagePullPolicy: IfNotPresent
      command: ["sh", "-c", "sleep 3600"]
      volumeMounts:
        - name: data
          mountPath: {{ .MountPath }}
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: {{ .Name }}
`))

func renderStatePodManifest(t *testing.T, data statePodManifestOpts) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, statePodManifestTmpl.Execute(&buf, data))
	return buf.String()
}

var statefulSetManifestTmpl = template.Must(template.New("sts").Parse(`
---
apiVersion: v1
kind: Service
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  clusterIP: None
  selector:
    app: {{ .Name }}
  ports:
    - name: tcp
      port: 80
      targetPort: 80

---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  serviceName: {{ .Name }}
  replicas: {{ .Replicas }}
  selector:
    matchLabels:
      app: {{ .Name }}
  template:
    metadata:
      labels:
        app: {{ .Name }}
    spec:
      terminationGracePeriodSeconds: 0
      containers:
        - name: verifier
          image: python:3.12-alpine
          imagePullPolicy: IfNotPresent
          command: ["sh", "-c", "sleep 3600"]
          volumeMounts:
            - name: data
              mountPath: {{ .MountPath }}
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 1Gi
`))

func renderStatefulSetManifest(t *testing.T, data statefulSetManifestOpts) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, statefulSetManifestTmpl.Execute(&buf, data))
	return buf.String()
}

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

// remote utils

type TreeEntry struct {
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
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

func readRemoteTree(t *testing.T, ns, pod, root string) map[string]TreeEntry {
	t.Helper()

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

	out := execInPod(t, ns, pod, script)

	var entries []TreeEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entries))

	m := make(map[string]TreeEntry, len(entries))
	for _, it := range entries {
		m[it.Path] = it
	}
	return m
}

// local utils

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

func writeRemoteFiles(t *testing.T, ns, pod, mountPath string, files map[string]string) {
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

	execInPod(t, ns, pod, script)
}

// check that syncpod made cleanup

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

// sts helpers

func clearRemoteDir(t *testing.T, ns, pod, root string) {
	t.Helper()

	execInPod(t, ns, pod, fmt.Sprintf(`
set -eu
mkdir -p %q
find %q -mindepth 1 -delete
`, root, root))
}

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
