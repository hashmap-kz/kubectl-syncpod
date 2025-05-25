package pipe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/pkg/clients"
	"github.com/pkg/sftp"
)

type downloadJob struct {
	RemotePath string
	LocalPath  string
}

func Download(host string, port int, remote, local, mountPath string) error {
	if err := waitForSSHReady(host, port, 30*time.Second); err != nil {
		return err
	}

	client, err := clients.NewSSHClient(host, port, "root", "root", "", "")
	if err != nil {
		return err
	}
	defer client.Close()
	sfptClient, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sfptClient.Close()

	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))
	local = filepath.ToSlash(filepath.Clean(local))
	err = downloadRecursive(sfptClient, remotePath, local)
	return err
}

func waitForSSHReady(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := clients.NewSSHClient(host, port, "root", "root", "", "")
		if err == nil {
			client.Close()
			return nil
		}
	}
	return fmt.Errorf("sshd not ready on %s:%d after %v", host, port, timeout)
}

func downloadRecursive(client *sftp.Client, remotePath, localPath string) error {
	const workerCount = 4
	jobs := make(chan downloadJob, 64)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if err := downloadFile(client, job.RemotePath, job.LocalPath); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				fmt.Println("Downloaded:", job.RemotePath, "→", job.LocalPath)
			}
		}()
	}

	// Walk the SFTP tree
	walker := client.Walk(remotePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}
		relPath, _ := filepath.Rel(remotePath, walker.Path())
		localFilePath := filepath.Join(localPath, relPath)

		if walker.Stat().IsDir() {
			if err := os.MkdirAll(localFilePath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", localFilePath, err)
			}
			continue
		}

		select {
		case jobs <- downloadJob{RemotePath: walker.Path(), LocalPath: localFilePath}:
		case err := <-errCh:
			close(jobs)
			return err
		}
	}

	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func downloadFile(client *sftp.Client, remotePath, localPath string) error {
	srcFile, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote: %w", err)
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for file: %w", err)
	}

	dstFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	return nil
}
