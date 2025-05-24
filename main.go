package main

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	helperContainer = "helper"
	helperImage     = "alpine"
)

func main() {
	var namespace, pvcName, mountPath string

	rootCmd := &cobra.Command{Use: "kubectl-syncpod"}

	uploadCmd := &cobra.Command{
		Use:   "upload <local-dir> <remote-path>",
		Short: "Upload local files to a PVC via temporary pod",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("upload", pvcName, namespace, args[0], args[1], mountPath)
		},
	}
	downloadCmd := &cobra.Command{
		Use:   "download <remote-path> <local-dir>",
		Short: "Download files from a PVC via temporary pod",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("download", pvcName, namespace, args[1], args[0], mountPath)
		},
	}

	uploadCmd.Flags().StringVar(&namespace, "namespace", "default", "Namespace")
	uploadCmd.Flags().StringVar(&pvcName, "pvc", "", "PVC name (required)")
	uploadCmd.Flags().StringVar(&mountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	downloadCmd.Flags().StringVar(&mountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	downloadCmd.Flags().StringVar(&namespace, "namespace", "default", "Namespace")
	downloadCmd.Flags().StringVar(&pvcName, "pvc", "", "PVC name (required)")

	for _, req := range []string{"pvc", "mount-path"} {
		_ = uploadCmd.MarkFlagRequired(req)
		_ = downloadCmd.MarkFlagRequired(req)
	}

	rootCmd.AddCommand(uploadCmd, downloadCmd)
	_ = rootCmd.Execute()
}

func run(mode, pvc, namespace, local, remote, mountPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

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
	defer deleteHelperPod(ctx, client, namespace, podName)

	switch mode {
	case "upload":
		return streamUpload(podName, helperContainer, namespace, local, remote, mountPath)
	case "download":
		return streamDownload(podName, helperContainer, namespace, remote, local, mountPath)
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

func createHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, pvc, mountPath string) (string, error) {
	podName := "syncpod-helper-" + randString(5)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    helperContainer,
					Image:   helperImage,
					Command: []string{"sleep", "3600"},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: mountPath},
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

func deleteHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, name string) {
	_ = client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func streamUpload(pod, container, ns, local, remote, mountPath string) error {
	cmd := exec.Command("kubectl", "exec", "-i", pod, "-c", container, "-n", ns,
		"--", "tar", "xzf", "-", "-C", filepath.Join(mountPath, remote))
	tarCmd := exec.Command("tar", "czf", "-", "-C", local, ".")
	pipe, err := tarCmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stdin = pipe

	if err := tarCmd.Start(); err != nil {
		return err
	}
	if err := cmd.Run(); err != nil {
		return err
	}
	return tarCmd.Wait()
}

func streamDownload(pod, container, ns, remote, local, mountPath string) error {
	cmd := exec.Command("kubectl", "exec", "-i", pod, "-c", container, "-n", ns,
		"--", "tar", "czf", "-", "-C", filepath.Join(mountPath, remote), ".")
	tarCmd := exec.Command("tar", "xzf", "-", "-C", local)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	tarCmd.Stdin = pipe

	if err := cmd.Start(); err != nil {
		return err
	}
	if err := tarCmd.Run(); err != nil {
		return err
	}
	return cmd.Wait()
}

func randString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
