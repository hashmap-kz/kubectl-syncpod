package pipe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
	"github.com/pkg/sftp"
)

func Upload(ctx context.Context, opts *JobOpts) error {
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
		if err := client.Close(); err != nil {
			slog.Error("error closing SFTP client", slog.Any("err", err))
		} else {
			slog.Info("SFTP connection closed")
		}
	}(client)

	localPath := filepath.Clean(opts.Local)
	remotePath := filepath.ToSlash(filepath.Join(opts.MountPath, filepath.Clean(opts.Remote)))

	slog.Info("begin to upload files",
		slog.String("local", localPath),
		slog.String("remote", remotePath),
	)

	err = uploadFiles(ctx, client.SFTPClient(), localPath, remotePath, opts.Workers)
	if err != nil {
		slog.Error("error while uploading files", slog.Any("err", err))
	} else {
		slog.Info("upload job completed successfully")
	}
	return err
}

func getFilesToUpload(client *sftp.Client, localPath, remotePath string) ([]workerJob, error) {
	var jobs []workerJob
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
		target := filepath.ToSlash(filepath.Join(remotePath, rel))

		isDir := d.IsDir()
		job := workerJob{
			LocalPath:  path,
			RemotePath: target,
			IsDir:      isDir,
		}

		fileExists, err := remoteFileExists(client, target, isDir)
		if err != nil {
			return err
		}
		if fileExists {
			return fmt.Errorf("override is forbidden, file already exists: %s", target)
		}

		if !isDir {
			localHash, err := sha256LocalFile(path)
			if err == nil {
				job.LocalHash = localHash
			}
			remoteHash, err := sha256RemoteFile(client, target)
			if err == nil {
				job.RemoteHash = remoteHash
			}
		}

		if job.IsDir || job.LocalHash != job.RemoteHash {
			jobs = append(jobs, job)
		}
		return nil
	})
	return jobs, err
}

func remoteFileExists(client *sftp.Client, path string, isDir bool) (bool, error) {
	stat, err := client.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// to avoid false positive, we don't know whether the file exists or not:
		// the safest way: we assume it exists
		return true, err
	}
	if isDir {
		return stat.IsDir(), nil
	}
	return !stat.IsDir(), nil
}

func uploadFiles(ctx context.Context, client *sftp.Client, localPath, remotePath string, workers int) error {
	files, err := getFilesToUpload(client, localPath, remotePath)
	if err != nil {
		return err
	}
	if workers <= 0 {
		workers = 1
	}

	slog.Info("starting concurrent file upload",
		slog.Int("workers", workers),
		slog.Int("files", len(files)),
	)

	jobs := make(chan workerJob, len(files))
	errCh := make(chan error, len(files))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for jb := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := uploadFile(client, jb); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}
		}()
	}

	for _, jb := range files {
		jobs <- jb
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(errCh)
	}()

	var lastErr error
	for e := range errCh {
		slog.Error("file upload error", slog.Any("err", e))
		lastErr = e
	}
	return lastErr
}

func uploadFile(client *sftp.Client, jb workerJob) error {
	localPath := filepath.ToSlash(jb.LocalPath)
	remotePath := filepath.ToSlash(jb.RemotePath)

	if jb.IsDir {
		return client.MkdirAll(remotePath)
	}

	slog.Debug("upload file",
		slog.String("remote", remotePath),
		slog.String("local", localPath),
	)

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
