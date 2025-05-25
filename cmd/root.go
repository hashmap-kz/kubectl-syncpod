package cmd

import (
	"flag"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func NewRootCmd(streams genericiooptions.IOStreams) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "kubectl syncpod",
		Short:        "Download/Upload files from a PVC via temporary pod",
		SilenceUsage: true,
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "no-help",
		Hidden: true,
	})

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.AddCommand(newDownloadCmd(streams))
	rootCmd.AddCommand(newUploadCmd(streams))
	return rootCmd
}
