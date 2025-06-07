package pipe

import (
	"fmt"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
)

type workerJob struct {
	LocalPath  string
	RemotePath string
	IsDir      bool
}

type JobOpts struct {
	Host           string
	Port           int
	Local          string
	Remote         string
	MountPath      string
	Workers        int
	KeyPair        *clients.KeyPair
	AllowOverwrite bool
	ObjName        string
	Namespace      string
	Owner          string
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
