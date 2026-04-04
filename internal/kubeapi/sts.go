package kubeapi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
