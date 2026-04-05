package integration

import (
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

// render test manifests

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

func renderStatePodManifest(t *testing.T, data statePodManifestOpts) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, statePodManifestTmpl.Execute(&buf, data))
	return buf.String()
}

func renderStatefulSetManifest(t *testing.T, data statefulSetManifestOpts) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, statefulSetManifestTmpl.Execute(&buf, data))
	return buf.String()
}
