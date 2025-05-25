package main

import (
	"log/slog"
	"os"

	"github.com/hashmap-kz/kubectl-syncpod/cmd"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func main() {
	streams := genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	rootCmd := cmd.NewRootCmd(streams)
	if err := rootCmd.Execute(); err != nil {
		slog.Error("error executing command", slog.Any("err", err))
		os.Exit(1)
	}
}
