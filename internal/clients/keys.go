package clients

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

type KeyPair struct {
	PublicKey                ed25519.PublicKey
	PrivateKey               ed25519.PrivateKey
	PublicKeyEncodedToString string
}

func GenerateEd25519Keys() (*KeyPair, error) {
	const sshAlgoType = "ssh-ed25519"

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	pubPublicKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, err
	}

	sshPubKey := sshAlgoType + " " + base64.StdEncoding.EncodeToString(pubPublicKey.Marshal())
	return &KeyPair{
		PublicKey:                pubKey,
		PublicKeyEncodedToString: sshPubKey,
		PrivateKey:               privKey,
	}, nil
}

func (k *KeyPair) PrivateKeyToPEM() ([]byte, error) {
	keyBytes, err := x509.MarshalPKCS8PrivateKey(k.PrivateKey)
	if err != nil {
		return nil, err
	}

	keyBuf := &bytes.Buffer{}
	err = pem.Encode(keyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	if err != nil {
		return nil, err
	}

	return keyBuf.Bytes(), nil
}
