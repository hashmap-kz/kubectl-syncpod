package cmd

import (
	"context"
	"flag"

	"github.com/hashmap-kz/kubectl-syncpod/internal/logger"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func NewRootCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "kubectl syncpod",
		Short:        "Download/Upload files from a PVC via temporary pod",
		SilenceUsage: true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			// TODO: --log-level, etc...
			logger.Init(&logger.Opts{
				Level:     "debug",
				Format:    "text",
				AddSource: false,
			})
		},
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "no-help",
		Hidden: true,
	})

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.AddCommand(newDownloadCmd(ctx, streams))
	rootCmd.AddCommand(newUploadCmd(ctx, streams))
	return rootCmd
}
