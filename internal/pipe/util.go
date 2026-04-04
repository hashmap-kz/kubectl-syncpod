package pipe

import (
	"fmt"
	"strings"
	"time"

	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
	"k8s.io/apimachinery/pkg/api/errors"
)

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

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	return errors.NewBadRequest(strings.Join(msgs, "\n"))
}
