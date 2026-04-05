package cmd

import (
	"context"
	"flag"

	"github.com/hashmap-kz/kubectl-syncpod/internal/logger"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func NewRootCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	logOpts := logger.Opts{}

	rootCmd := &cobra.Command{
		Use:          "kubectl syncpod",
		Short:        "Download/Upload files from a PVC via temporary pod",
		SilenceUsage: true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			logger.Init(&logOpts)
		},
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "no-help",
		Hidden: true,
	})

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	cfg.AddFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().StringVar(&logOpts.Level, "log-level", "info", "Log level (trace, debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&logOpts.Format, "log-format", "text", "Log format (text, json)")
	rootCmd.PersistentFlags().BoolVar(&logOpts.AddSource, "log-add-source", false, "Add source file/line to log output")

	rootCmd.AddCommand(newDownloadCmd(ctx, cfg, streams))
	rootCmd.AddCommand(newUploadCmd(ctx, cfg, streams))
	rootCmd.AddCommand(newDownloadSTSCmd(ctx, cfg, streams))
	rootCmd.AddCommand(newUploadSTSCmd(ctx, cfg, streams))
	return rootCmd
}
