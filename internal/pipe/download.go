package pipe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"

	"github.com/pkg/sftp"
)

const (
	sshWaitTimeout = 30 * time.Second
)

func Download(ctx context.Context, opts *JobOpts) error {
	slog.Info("waiting while SSHD is ready")
	if err := waitForSSHReady(opts.KeyPair, opts.Host, opts.Port, sshWaitTimeout); err != nil {
		return err
	}

	slog.Info("init SSH client")
	client, err := newSFTPClient(opts.KeyPair, opts.Host, opts.Port)
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

	remotePath := filepath.ToSlash(filepath.Join(opts.MountPath, filepath.Clean(opts.Remote)))
	local := filepath.ToSlash(filepath.Clean(opts.Local))

	// ensure destination dir
	if err := os.MkdirAll(filepath.ToSlash(local), 0o750); err != nil {
		return err
	}

	slog.Info("begin to download files",
		slog.String("remote", remotePath),
		slog.String("local", local),
	)
	err = downloadFiles(ctx, client.SFTPClient(), remotePath, local, opts.Workers)
	if err != nil {
		slog.Error("error while downloading files", slog.Any("err", err))
	} else {
		slog.Info("download job completed successfully")
	}
	return err
}

func getFilesToDownload(client *sftp.Client, remotePath, localPath string) ([]workerJob, error) {
	var jobs []workerJob

	walker := client.Walk(remotePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return nil, err
		}

		relPath, err := filepath.Rel(remotePath, walker.Path())
		if err != nil {
			return nil, err
		}
		localFilePath := filepath.Join(localPath, relPath)

		isDir := walker.Stat().IsDir()
		job := workerJob{
			RemotePath: walker.Path(),
			LocalPath:  localFilePath,
			IsDir:      isDir,
		}

		if !isDir {
			if _, err := os.Stat(localFilePath); err == nil {
				localHash, err := sha256LocalFile(localFilePath)
				if err == nil {
					job.LocalHash = localHash
				}
			}
			remoteHash, err := sha256RemoteFile(client, walker.Path())
			if err == nil {
				job.RemoteHash = remoteHash
			}
		}

		if job.IsDir || job.LocalHash != job.RemoteHash {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func downloadFiles(ctx context.Context, client *sftp.Client, remotePath, localPath string, workers int) error {
	files, err := getFilesToDownload(client, remotePath, localPath)
	if err != nil {
		return err
	}

	if workers <= 0 {
		workers = 1
	}

	slog.Info("starting concurrent file download",
		slog.Int("workers", workers),
		slog.Int("files", len(files)),
	)

	filesChan := make(chan workerJob, len(files))
	errorChan := make(chan error, len(files))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for jb := range filesChan {
				if ctx.Err() != nil {
					return
				}
				err := downloadFile(client, jb)
				if err != nil {
					select {
					case errorChan <- err:
					default:
					}
				}
			}
		}()
	}

	// Send found files to worker chan
	for _, path := range files {
		filesChan <- path
	}
	close(filesChan) // Close the task channel once all tasks are submitted

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(errorChan)
	}()

	var lastErr error
	for e := range errorChan {
		slog.Error("file download error",
			slog.Any("err", e),
		)
		lastErr = e
	}
	return lastErr
}

func downloadFile(client *sftp.Client, jb workerJob) error {
	remotePath := filepath.ToSlash(jb.RemotePath)
	localPath := filepath.ToSlash(jb.LocalPath)

	if jb.IsDir {
		return os.MkdirAll(localPath, 0o750)
	}

	slog.Debug("download file",
		slog.String("remote", remotePath),
		slog.String("local", localPath),
	)

	srcFile, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote: %w", err)
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.ToSlash(filepath.Dir(localPath)), 0o750); err != nil {
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
