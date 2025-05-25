package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/internal/pipe"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	helperContainer = "helper"
	helperImage     = "alpine"
	helperService   = "syncpod-sshd"

	// TODO: nodeIP + nodePort
	host = "10.40.240.165"
	port = 30154
)

func run(ctx context.Context, mode, pvc, namespace, local, remote, mountPath string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return fmt.Errorf("load kubeconfig: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	podName, err := createHelperPod(ctx, client, namespace, pvc, mountPath)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deleteHelperPod(cleanupCtx, client, namespace, podName); err != nil {
			slog.Error("cannot delete helper pod", slog.Any("err", err))
		}
	}()

	err = createNodePortService(ctx, client, namespace, helperService, 30154)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deleteHelperService(cleanupCtx, client, namespace, helperService); err != nil {
			slog.Error("cannot delete helper service", slog.Any("err", err))
		}
	}()

	switch mode {
	case "upload":
		return pipe.Upload(host, port,
			filepath.ToSlash(local),
			filepath.ToSlash(remote),
			filepath.ToSlash(mountPath),
		)
	case "download":
		return pipe.Download(host, port,
			filepath.ToSlash(remote),
			filepath.ToSlash(local),
			filepath.ToSlash(mountPath),
		)
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

func createHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, pvc, mountPath string) (string, error) {
	pvcNodeName, err := getPVCNodeName(ctx, client, namespace, pvc)
	if err != nil {
		return "", err
	}

	podName := "syncpod-helper-" + randString(7)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    labels(),
		},
		Spec: corev1.PodSpec{
			NodeName:              pvcNodeName,
			ActiveDeadlineSeconds: pointerToInt64(86400 / 2), // TODO: configure
			RestartPolicy:         corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  helperContainer,
					Image: helperImage,
					Command: []string{"sh", "-c", `
  apk update && apk add openssh &&
  echo "root:root" | chpasswd &&
  echo "PermitRootLogin yes" >> /etc/ssh/sshd_config &&
  ssh-keygen -A &&
  /usr/sbin/sshd -D -p 2222
`},

					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: mountPath,
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 2222,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc,
						},
					},
				},
			},
		},
	}
	_, err = client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	for {
		p, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if p.Status.Phase == corev1.PodRunning {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return podName, nil
}

func createNodePortService(ctx context.Context, client *kubernetes.Clientset, namespace, serviceName string, nodePort int32) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    labels(),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: labels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "ssh",
					Port:       2222,                // Exposed port
					TargetPort: intstrFromInt(2222), // Inside container
					NodePort:   nodePort,            // e.g. 32222
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	fmt.Printf("NodePort service %q created. Connect using: sftp -P %d sync@<node-ip>\n", serviceName, nodePort)
	return nil
}

func labels() map[string]string {
	return map[string]string{
		"app": helperService,
	}
}

func intstrFromInt(i int) intstr.IntOrString {
	return intstr.IntOrString{Type: intstr.Int, IntVal: int32(i)}
}

func deleteHelperService(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	err := client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	slog.Info("helper service deleted", slog.String("name", name))
	return nil
}

func deleteHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	err := client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	slog.Info("helper pod deleted", slog.String("name", name))
	return nil
}

func getPVCNodeName(ctx context.Context, client *kubernetes.Clientset, namespace, pvcName string) (string, error) {
	// 1) Try to find a pod that uses the PVC
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing pods: %w", err)
	}

	for pi := range pods.Items {
		pod := pods.Items[pi]
		for vi := range pod.Spec.Volumes {
			vol := pod.Spec.Volumes[vi]
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
				if pod.Spec.NodeName != "" {
					return pod.Spec.NodeName, nil // Fast path
				}
			}
		}
	}

	// 2) Fallback - check PVC -> PV -> NodeAffinity
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get PVC: %w", err)
	}
	if pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" {
		return "", fmt.Errorf("PVC %s is not bound to any PV", pvcName)
	}

	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get PV %s: %w", pvc.Spec.VolumeName, err)
	}

	if pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "kubernetes.io/hostname" &&
					expr.Operator == corev1.NodeSelectorOpIn &&
					len(expr.Values) > 0 {
					return expr.Values[0], nil // Fallback path
				}
			}
		}
	}

	return "", fmt.Errorf("unable to determine node for PVC %q", pvcName)
}

func randString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, n)
	for i := range s {
		//nolint:gosec
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

func pointerToInt64(i int64) *int64 {
	return &i
}
