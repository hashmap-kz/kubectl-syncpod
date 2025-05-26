package clients

import (
	"fmt"
	"net"
	"os"

	"github.com/pkg/sftp"

	"golang.org/x/crypto/ssh"
)

type SFTPConfig struct {
	Host string
	Port int
	User string
	Pass string

	PkeyBytes []byte
	PkeyPath  string
	PkeyPass  string // Optional, it private key is created with a passphrase
}

type SFTPClient struct {
	sshClient  *ssh.Client
	sftpClient *sftp.Client
	config     *SFTPConfig
}

func NewSFTPClient(cfg *SFTPConfig) (*SFTPClient, error) {
	authMethods, err := getAuthsMethods(cfg.Pass, cfg.PkeyBytes, cfg.PkeyPath, cfg.PkeyPass)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), config)
	if sshClient == nil || err != nil {
		return nil, err
	}

	_, _, err = sshClient.SendRequest(fmt.Sprintf("%s@%s", cfg.User, cfg.Host), true, nil)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, err
	}

	return &SFTPClient{
		sshClient:  sshClient,
		sftpClient: sftpClient,
		config:     nil,
	}, nil
}

func (s *SFTPClient) SFTPClient() *sftp.Client {
	return s.sftpClient
}

func (s *SFTPClient) Close() error {
	var err error
	if s.sftpClient != nil {
		err = s.sftpClient.Close()
	}
	if s.sshClient != nil {
		err = s.sshClient.Close()
	}
	return err
}

// internal

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
func getAuthsMethods(password string, privateKeyBytes []byte, privateKeyFilename, privateKeyPassphrase string) ([]ssh.AuthMethod, error) {
	var auths []ssh.AuthMethod
	var err error

	if password != "" {
		auths = append(auths, ssh.Password(password))
	} else {
		var key []byte
		if privateKeyBytes != nil {
			key = privateKeyBytes
		} else if privateKeyFilename != "" {
			key, err = os.ReadFile(privateKeyFilename)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("neither pkey-bytes nor pkey-path are defined")
		}

		signer, err := getSigner(key, privateKeyPassphrase)
		if err != nil {
			return nil, err
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	return auths, nil
}
