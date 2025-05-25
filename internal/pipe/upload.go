package pipe

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
	"github.com/pkg/sftp"
)

type uploadJob struct {
	LocalPath  string
	RemotePath string
}

func Upload(host string, port int, local, remote, mountPath string) error {
	slog.Info("waiting while SSHD is ready")
	if err := waitForSSHReady(host, port, sshWaitTimeout); err != nil {
		return err
	}

	slog.Info("init SSH client")
	client, err := newSFTPClient(host, port)
	if err != nil {
		return err
	}
	defer func(client *clients.SFTPClient) {
		err := client.Close()
		if err != nil {
			slog.Error("error closing SFTP client", slog.Any("err", err))
		} else {
			slog.Info("SFTP connection closed")
		}
	}(client)

	local = filepath.Clean(local)
	remotePath := filepath.ToSlash(filepath.Join(mountPath, filepath.Clean(remote)))

	slog.Info("begin to upload files", slog.String("local", local), slog.String("remote", remotePath))
	err = uploadFiles(client.SFTPClient(), local, remotePath)
	if err != nil {
		slog.Error("error while uploading files", slog.Any("err", err))
	} else {
		slog.Info("upload job completed successfully")
	}
	return err
}

func uploadFiles(client *sftp.Client, localPath, remotePath string) error {
	const workerCount = 4
	jobs := make(chan uploadJob, 64)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if err := uploadFile(client, filepath.ToSlash(job.LocalPath), filepath.ToSlash(job.RemotePath)); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				slog.Debug("upload",
					slog.String("local", filepath.ToSlash(job.LocalPath)),
					slog.String("remote", filepath.ToSlash(job.RemotePath)),
				)
			}
		}()
	}

	// Walk the local tree
	base := filepath.Base(localPath)
	err := filepath.WalkDir(localPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(localPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Join(base, rel))
		target := filepath.Join(remotePath, rel)

		if d.IsDir() {
			if err := client.MkdirAll(target); err != nil {
				return fmt.Errorf("mkdir remote: %w", err)
			}
			return nil
		}

		select {
		case jobs <- uploadJob{LocalPath: path, RemotePath: target}:
			return nil
		case err := <-errCh:
			return err
		}
	})
	if err != nil {
		close(jobs)
		return err
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

func uploadFile(client *sftp.Client, localPath, remotePath string) error {
	srcFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local: %w", err)
	}
	defer srcFile.Close()

	if err := client.MkdirAll(filepath.ToSlash(filepath.Dir(remotePath))); err != nil {
		return fmt.Errorf("mkdir remote: %w", err)
	}

	dstFile, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	return nil
}
