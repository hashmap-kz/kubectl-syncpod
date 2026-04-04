package kub

import (
	"strings"

	"github.com/google/uuid"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func ResolveNamespace(cfg *genericclioptions.ConfigFlags) string {
	namespace := "default"
	if cfg.Namespace != nil && strings.TrimSpace(*cfg.Namespace) != "" {
		namespace = *cfg.Namespace
	}
	return namespace
}

func NewObjName() string {
	return "syncpod-" + uuid.NewString()
}
