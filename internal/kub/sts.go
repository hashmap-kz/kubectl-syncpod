package kub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type PodVolume struct {
	PodName    string
	Ordinal    int
	VolumeName string
	PVCName    string
	MountPath  string
	Container  string
	ReadOnly   bool
}

func DiscoverStatefulSetPVCs(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, stsName string,
) ([]PodVolume, error) {
	sts, err := client.AppsV1().
		StatefulSets(namespace).
		Get(ctx, stsName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get statefulset: %w", err)
	}

	selector := labels.Set(sts.Spec.Selector.MatchLabels).AsSelector()

	pods, err := client.CoreV1().
		Pods(namespace).
		List(ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
		})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	var result []PodVolume

	for i := range len(pods.Items) {
		pod := pods.Items[i]
		ordinal := extractOrdinal(pod.Name)

		// Map volumeName -> PVCName
		pvcVolumes := map[string]string{}

		for i := range len(pod.Spec.Volumes) {
			v := pod.Spec.Volumes[i]
			if v.PersistentVolumeClaim != nil {
				pvcVolumes[v.Name] = v.PersistentVolumeClaim.ClaimName
			}
		}

		if len(pvcVolumes) == 0 {
			continue
		}

		for i := range len(pod.Spec.Containers) {
			c := pod.Spec.Containers[i]
			for _, m := range c.VolumeMounts {
				pvcName, ok := pvcVolumes[m.Name]
				if !ok {
					continue // not a PVC-backed volume
				}

				result = append(result, PodVolume{
					PodName:    pod.Name,
					Ordinal:    ordinal,
					VolumeName: m.Name,
					PVCName:    pvcName,
					MountPath:  m.MountPath,
					Container:  c.Name,
					ReadOnly:   m.ReadOnly,
				})
			}
		}
	}

	// Optional: sort for deterministic output
	sort.Slice(result, func(i, j int) bool {
		if result[i].PodName != result[j].PodName {
			return result[i].PodName < result[j].PodName
		}
		return result[i].VolumeName < result[j].VolumeName
	})

	return result, nil
}

// manifest

type StatefulSetBackupManifest struct {
	Version     int                 `json:"version"`
	Kind        string              `json:"kind"`
	Namespace   string              `json:"namespace"`
	StatefulSet string              `json:"statefulset"`
	CapturedAt  time.Time           `json:"captured_at"`
	Entries     []StatefulSetVolume `json:"entries"`
}

type StatefulSetVolume struct {
	PodName    string `json:"pod_name"`
	Ordinal    int    `json:"ordinal"`
	VolumeName string `json:"volume_name"`
	PVCName    string `json:"pvc_name"`
	MountPath  string `json:"mount_path"`
	Container  string `json:"container,omitempty"`
	ReadOnly   bool   `json:"read_only,omitempty"`
	LocalPath  string `json:"local_path"`
}

func BuildStatefulSetBackupManifest(namespace, sts string, vols []PodVolume) *StatefulSetBackupManifest {
	entries := make([]StatefulSetVolume, 0, len(vols))

	for _, v := range vols {
		entries = append(entries, StatefulSetVolume{
			PodName:    v.PodName,
			Ordinal:    v.Ordinal,
			VolumeName: v.VolumeName,
			PVCName:    v.PVCName,
			MountPath:  v.MountPath,
			Container:  v.Container,
			ReadOnly:   v.ReadOnly,
			LocalPath:  filepath.ToSlash(filepath.Join(v.PodName, v.VolumeName)),
		})
	}

	return &StatefulSetBackupManifest{
		Version:     1,
		Kind:        "StatefulSetBackupManifest",
		Namespace:   namespace,
		StatefulSet: sts,
		CapturedAt:  time.Now().UTC(),
		Entries:     entries,
	}
}

func WriteStatefulSetBackupManifest(path string, m *StatefulSetBackupManifest) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return nil
}

func ReadStatefulSetBackupManifest(path string) (*StatefulSetBackupManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var m StatefulSetBackupManifest
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	if m.Kind != "StatefulSetBackupManifest" {
		return nil, fmt.Errorf("unexpected manifest kind %q", m.Kind)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("unsupported manifest version %d", m.Version)
	}

	return &m, nil
}

// helpers

func extractOrdinal(podName string) int {
	parts := strings.Split(podName, "-")
	if len(parts) == 0 {
		return -1
	}

	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return -1
	}

	return n
}
