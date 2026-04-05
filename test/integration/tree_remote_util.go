package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// operations in POD, with PVC, etc

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

func clearRemoteDir(t *testing.T, ns, pod, root string) {
	t.Helper()

	execInPod(t, ns, pod, fmt.Sprintf(`
set -eu
mkdir -p %q
find %q -mindepth 1 -delete
`, root, root))
}
