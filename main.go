package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

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

	rootCmd := &cobra.Command{
		Use:          "kubectl-syncpod",
		SilenceUsage: true,
	}

	uploadCmd := &cobra.Command{
		Use:          "upload <local-dir> <remote-path>",
		Short:        "Upload local files to a PVC via temporary pod",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run("upload", pvcName, namespace, args[0], args[1], mountPath)
		},
	}
	downloadCmd := &cobra.Command{
		Use:          "download <remote-path> <local-dir>",
		Short:        "Download files from a PVC via temporary pod",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
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
		return streamUpload(podName, helperContainer, namespace,
			filepath.ToSlash(local),
			filepath.ToSlash(remote),
			filepath.ToSlash(mountPath),
		)
	case "download":
		return streamDownloadExecAPI(config, client, podName, helperContainer, namespace,
			filepath.ToSlash(remote),
			filepath.ToSlash(local),
			filepath.ToSlash(mountPath),
		)
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

func createHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, pvc, mountPath string) (string, error) {
	// TODO: CLI --target-node
	pvcNodeName, _ := getPVCNodeName(ctx, client, namespace, pvc)

	podName := "syncpod-helper-" + randString(7)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName:              pvcNodeName,
			ActiveDeadlineSeconds: pointerToInt64(86400 / 2), // TODO: configure
			RestartPolicy:         corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    helperContainer,
					Image:   helperImage,
					Command: []string{"sleep", "infinity"},
					// Command: []string{"tail", "-f", "/dev/null"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: mountPath,
							// ReadOnly:  true,
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

func getPVCNodeName(ctx context.Context, client *kubernetes.Clientset, namespace, pvcName string) (string, error) {
	// 1) Try to find a pod that uses the PVC
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing pods: %w", err)
	}

	for _, pod := range pods.Items {
		for _, vol := range pod.Spec.Volumes {
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

func streamUpload0(pod, container, ns, local, remote, mountPath string) error {
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

func streamUpload(pod, container, ns, local, remote, mountPath string) error {
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))
	local = filepath.ToSlash(filepath.Clean(local))

	// Debug: show commands being run
	fmt.Println("Running:", "tar czf - -C", local, ".", "|", "kubectl exec", pod, "-- tar xzf - -C", remotePath)

	// Upload local dir to remote PVC path via helper pod
	cmd := exec.Command("kubectl", "exec", "-i", pod, "-c", container, "-n", ns,
		"--", "tar", "xzf", "-", "-C", remotePath)

	tarCmd := exec.Command("tar", "czf", "-", "-C", local, ".")

	// Pipe tar output to kubectl stdin
	pipe, err := tarCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	cmd.Stdin = pipe

	// Stream stderr for both processes
	cmd.Stderr = os.Stderr
	tarCmd.Stderr = os.Stderr

	if err := tarCmd.Start(); err != nil {
		return fmt.Errorf("start local tar: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start kubectl exec: %w", err)
	}

	if err := tarCmd.Wait(); err != nil {
		return fmt.Errorf("local tar failed: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("kubectl exec failed: %w", err)
	}

	return nil
}

func randString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

func pointerToInt64(i int64) *int64 {
	return &i
}

/////// download ///////

// download-v1

func streamDownload0(pod, container, ns, remote, local, mountPath string) error {
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

// download-v2

func streamDownload(pod, container, ns, remote, local, mountPath string) error {
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))
	local = filepath.ToSlash(filepath.Clean(local))

	cmd := exec.Command("kubectl", "exec", "-i", pod, "-c", container, "-n", ns,
		"--", "tar", "czf", "-", "-C", remotePath, ".")
	tarCmd := exec.Command("tar", "xzf", "-", "-C", local)

	// Debug: print command args
	fmt.Println("Running:", cmd.String(), "| tar xzf - -C", local)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("get stdout pipe: %w", err)
	}
	tarCmd.Stdin = stdout

	// Capture stderr
	cmd.Stderr = os.Stderr
	tarCmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start kubectl exec: %w", err)
	}
	if err := tarCmd.Start(); err != nil {
		return fmt.Errorf("start tar: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("kubectl exec failed: %w", err)
	}
	if err := tarCmd.Wait(); err != nil {
		return fmt.Errorf("tar extract failed: %w", err)
	}

	return nil
}

// download-v3

func streamDownloadExecAPI(config *rest.Config, clientset *kubernetes.Clientset,
	pod, container, namespace, remote, local, mountPath string,
) error {
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))
	local = filepath.ToSlash(filepath.Clean(local))

	// Exec into the pod to run: tar czf - -C <remotePath> .
	cmd := []string{"tar", "czf", "-", "-C", remotePath, "."}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
			Stdin:     false,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}

	// Create a pipe to receive the tar.gz stream
	pr, pw := io.Pipe()

	// Start untar goroutine
	done := make(chan error, 1)
	go func() {
		defer pr.Close()
		gr, err := gzip.NewReader(pr)
		if err != nil {
			done <- fmt.Errorf("create gzip reader: %w", err)
			return
		}
		defer gr.Close()

		tr := tar.NewReader(gr)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				done <- fmt.Errorf("read tar stream: %w", err)
				return
			}

			target := filepath.Join(local, header.Name)
			log.Printf("download target: %s\n", target)

			switch header.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
					done <- fmt.Errorf("mkdir %s: %w", target, err)
					return
				}
			case tar.TypeReg:
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					done <- fmt.Errorf("mkdir parent: %w", err)
					return
				}
				out, err := os.Create(target)
				if err != nil {
					done <- fmt.Errorf("create file: %w", err)
					return
				}
				if _, err := io.Copy(out, tr); err != nil {
					out.Close()
					done <- fmt.Errorf("write file: %w", err)
					return
				}
				out.Close()
				if err := os.Chmod(target, os.FileMode(header.Mode)); err != nil {
					done <- fmt.Errorf("chmod: %w", err)
					return
				}
			default:
				// skip other types like symlinks
				continue
			}
		}
		done <- nil
	}()

	// Start the remote stream (tar czf -)
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: pw,
		Stderr: os.Stderr,
	})
	pw.Close()

	if err != nil {
		return fmt.Errorf("remote tar exec failed: %w", err)
	}

	if err := <-done; err != nil {
		return fmt.Errorf("untar failed: %w", err)
	}

	return nil
}
