package pipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
)

type workerJob struct {
	LocalPath  string
	RemotePath string
	IsDir      bool
	LocalHash  string
	RemoteHash string
}

type JobOpts struct {
	Host      string
	Port      int
	Local     string
	Remote    string
	MountPath string
	Workers   int
	KeyPair   *clients.KeyPair
}

func waitForSSHReady(keyPair *clients.KeyPair, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := newSFTPClient(keyPair, host, port)
		if err == nil {
			return client.Close()
		}
	}
	return fmt.Errorf("sshd not ready on %s:%d after %v", host, port, timeout)
}

func newSFTPClient(keyPair *clients.KeyPair, host string, port int) (*clients.SFTPClient, error) {
	privateKeyToPEM, err := keyPair.PrivateKeyToPEM()
	if err != nil {
		return nil, err
	}
	return clients.NewSFTPClient(&clients.SFTPConfig{
		Host:      host,
		Port:      port,
		User:      "root",
		PkeyBytes: privateKeyToPEM,
	})
}

// hashing

func sha256File(r io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func sha256LocalFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256File(f)
}

func sha256RemoteFile(client *sftp.Client, path string) (string, error) {
	f, err := client.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256File(f)
}
