package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashmap-kz/kubectl-syncpod/internal/logger"

	"github.com/hashmap-kz/kubectl-syncpod/cmd"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Init(&logger.Opts{
		Level:     "info",
		Format:    "text",
		AddSource: false,
	})

	streams := genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	rootCmd := cmd.NewRootCmd(ctx, streams)
	if err := rootCmd.Execute(); err != nil {
		slog.Error("error executing command", slog.Any("err", err))
	} else {
		slog.Info("all jobs completed successfully")
	}
}
