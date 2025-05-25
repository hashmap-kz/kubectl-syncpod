package cmd

import (
	"context"
	"fmt"
	"log/slog"
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
	helperImage = "alpine" // TODO: configure
	objName     = "syncpod-bab9e5b1-eaa7-4b3b-964e-103f0a4f8cc3"
)

var activeDeadlineSeconds int64 = 86400 / 2 // TODO: configure

type nodeInfo struct {
	name string
	addr string
}

func run(ctx context.Context, mode, pvc, namespace, local, remote, mountPath string) error {
	// config routine

	slog.Info("init k8s config")
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return fmt.Errorf("load kubeconfig: %w", err)
		}
	}

	slog.Info("init k8s client")
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	// node

	slog.Info("fetching target node to schedule pod on")
	node, err := getNodeInfo(ctx, client, namespace, pvc)
	if err != nil {
		return err
	}

	// pod

	slog.Info("creating pod")
	err = createHelperPod(ctx, client, namespace, pvc, mountPath, node.name)
	if err != nil {
		return err
	}
	slog.Info("pod created", slog.String("name", objName))
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deleteHelperPod(cleanupCtx, client, namespace); err != nil {
			slog.Error("cannot delete pod", slog.Any("err", err))
		} else {
			slog.Info("pod deleted", slog.String("name", objName))
		}
	}()

	// service

	slog.Info("creating service")
	port, err := createNodePortService(ctx, client, namespace)
	if err != nil {
		return err
	}
	slog.Info("service created",
		slog.String("name", objName),
		slog.Int64("port", int64(port)),
	)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deleteHelperService(cleanupCtx, client, namespace, objName); err != nil {
			slog.Error("cannot delete service", slog.Any("err", err))
		} else {
			slog.Info("service deleted", slog.String("name", objName))
		}
	}()

	switch mode {
	case "upload":
		return pipe.Upload(node.addr, int(port),
			filepath.ToSlash(local),
			filepath.ToSlash(remote),
			filepath.ToSlash(mountPath),
		)
	case "download":
		return pipe.Download(node.addr, int(port),
			filepath.ToSlash(remote),
			filepath.ToSlash(local),
			filepath.ToSlash(mountPath),
		)
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

// objects

func createHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, pvc, mountPath, pvcNodeName string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objName,
			Namespace: namespace,
			Labels:    labels(),
		},
		Spec: corev1.PodSpec{
			NodeName:              pvcNodeName,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			RestartPolicy:         corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  objName,
					Image: helperImage,
					Command: []string{"sh", "-c", `
  apk update && apk add openssh &&
  echo "root:root" | chpasswd &&
  echo "PermitRootLogin yes" >> /etc/ssh/sshd_config &&
  ssh-keygen -A &&
  /usr/sbin/sshd -D -p 2525
`},

					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: mountPath,
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 2525,
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
	_, err := client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	for {
		p, err := client.CoreV1().Pods(namespace).Get(ctx, objName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if p.Status.Phase == corev1.PodRunning {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

func createNodePortService(ctx context.Context, client *kubernetes.Clientset, namespace string) (int32, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objName,
			Namespace: namespace,
			Labels:    labels(),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: labels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "ssh",
					Port:       2525,                                                      // Exposed port
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(2525)}, // Inside container
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	_, err := client.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return -1, fmt.Errorf("create service: %w", err)
	}

	svcCreated, err := client.CoreV1().Services(namespace).Get(ctx, objName, metav1.GetOptions{})
	if err != nil {
		return -1, err
	}
	if len(svcCreated.Spec.Ports) == 0 {
		return -1, fmt.Errorf("cannot expose service")
	}
	nodePort := svcCreated.Spec.Ports[0].NodePort
	return nodePort, nil
}

// cleanup

func deleteHelperService(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	err := client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func deleteHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	err := client.CoreV1().Pods(namespace).Delete(ctx, objName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// node

func getNodeInfo(ctx context.Context, client *kubernetes.Clientset, namespace, pvc string) (*nodeInfo, error) {
	// get node name
	pvcNodeName, err := getPVCNodeName(ctx, client, namespace, pvc)
	if err != nil {
		return nil, err
	}

	// get node IP addr
	node, err := client.CoreV1().Nodes().Get(ctx, pvcNodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	addresses := node.Status.Addresses
	if len(addresses) == 0 {
		return nil, fmt.Errorf("cannot decide node IP: %s", pvcNodeName)
	}
	var nodeAddr string
	for _, addr := range addresses {
		if addr.Type == corev1.NodeInternalIP {
			nodeAddr = addr.Address
		}
	}
	if nodeAddr == "" {
		for _, addr := range addresses {
			if addr.Type == corev1.NodeHostName {
				nodeAddr = addr.Address
			}
		}
	}
	if nodeAddr == "" {
		return nil, fmt.Errorf("cannot decide node IP: %s", pvcNodeName)
	}

	slog.Info("decided node",
		slog.String("addr", nodeAddr),
		slog.String("name", pvcNodeName),
	)

	return &nodeInfo{
		name: pvcNodeName,
		addr: nodeAddr,
	}, nil
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

// utils

func labels() map[string]string {
	return map[string]string{
		"app": objName,
	}
}
