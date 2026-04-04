package dto

import (
	"github.com/hashmap-kz/kubectl-syncpod/internal/clients"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type WorkerJob struct {
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

	Client     kubernetes.Interface
	RestConfig *rest.Config
}
