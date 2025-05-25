package clients

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

func isPasswordProtectedPrivateKey(key []byte) bool {
	_, err := ssh.ParsePrivateKey(key)
	if err != nil {
		if err.Error() == (&ssh.PassphraseMissingError{}).Error() {
			return true
		}
	}
	return false
}

func getSigner(key []byte, passphrase string) (ssh.Signer, error) {
	if isPasswordProtectedPrivateKey(key) {
		if passphrase == "" {
			return nil, &ssh.PassphraseMissingError{}
		}

		signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
		return signer, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	return signer, err
}

// Authentication with password or private_key+optional(passphrase)
func getAuthsMethods(password string, privateKeyFilename string, privateKeyPassphrase string) ([]ssh.AuthMethod, error) {
	var auths []ssh.AuthMethod

	if password != "" {
		auths = append(auths, ssh.Password(password))
	} else {
		key, err := os.ReadFile(privateKeyFilename)
		if err != nil {
			return nil, err
		}

		signer, err := getSigner(key, privateKeyPassphrase)
		if err != nil {
			return nil, err
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	return auths, nil
}

func NewSSHClient(host string, port int, user string, password string, privateKeyFilename string, privateKeyPassphrase string) (*ssh.Client, error) {
	authMethods, err := getAuthsMethods(password, privateKeyFilename, privateKeyPassphrase)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), config)
	if client == nil || err != nil {
		return nil, err
	}

	_, _, err = client.SendRequest(fmt.Sprintf("%s@%s", user, host), true, nil)
	if err != nil {
		return nil, err
	}

	return client, nil
}
