package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	helperContainer = "helper"
	helperImage     = "alpine"
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
	defer deleteHelperPod(ctx, client, namespace, podName)

	switch mode {
	case "upload":
		return streamUploadExecAPI(ctx, config, client, podName, helperContainer, namespace,
			filepath.ToSlash(local),
			filepath.ToSlash(remote),
			filepath.ToSlash(mountPath),
		)
	case "download":
		return streamDownloadExecAPI(ctx, config, client, podName, helperContainer, namespace,
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

func deleteHelperPod(ctx context.Context, client *kubernetes.Clientset, namespace, name string) {
	err := client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		slog.Error("cannot delete helper pod", slog.Any("err", err))
	}
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

/////// download ///////

func streamDownloadExecAPI(
	ctx context.Context,
	config *rest.Config,
	clientset *kubernetes.Clientset,
	pod, container, namespace, remote, local, mountPath string,
) error {
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))
	local = filepath.ToSlash(filepath.Clean(local))

	cmd := []string{"tar", "czf", "-", "-C", filepath.ToSlash(filepath.Dir(remotePath)), filepath.Base(remotePath)}

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

	pr, pw := io.Pipe()
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
				continue
			}
		}
		done <- nil
	}()

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
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

/////// upload ///////

func streamUploadExecAPI(
	ctx context.Context,
	config *rest.Config,
	clientset *kubernetes.Clientset,
	pod, container, namespace, local, remote, mountPath string,
) error {
	local = filepath.Clean(local)
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))

	info, err := os.Stat(local)
	if err != nil {
		return fmt.Errorf("stat local path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local path must be a directory: %s", local)
	}
	base := filepath.Base(local)

	cmd := []string{"tar", "xzf", "-", "-C", remotePath}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		defer pw.Close()
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)

		err := filepath.WalkDir(local, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(local, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(filepath.Join(base, rel)) // preserve top-level dir

			if rel == base {
				// Emit top-level directory explicitly
				info, err := d.Info()
				if err != nil {
					return err
				}
				hdr, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return err
				}
				hdr.Name = rel
				return tw.WriteHeader(hdr)
			}

			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = rel

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()

				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			done <- fmt.Errorf("walk and write tar: %w", err)
			return
		}

		if err := tw.Close(); err != nil {
			done <- fmt.Errorf("close tar: %w", err)
			return
		}
		if err := gw.Close(); err != nil {
			done <- fmt.Errorf("close gzip: %w", err)
			return
		}
		done <- nil
	}()

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  pr,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	pr.Close()

	if err != nil {
		return fmt.Errorf("remote tar exec failed: %w", err)
	}
	if err := <-done; err != nil {
		return fmt.Errorf("tar stream write failed: %w", err)
	}

	return nil
}
