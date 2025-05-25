package cmd

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type DownloadOptions struct {
	MountPath string
	PVC       string
}

type downloadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        DownloadOptions
	args        []string
}

func newDownloadCmd(streams genericiooptions.IOStreams) *cobra.Command {
	opts := genericclioptions.NewConfigFlags(true)
	downloadOptions := DownloadOptions{}
	cmd := &cobra.Command{
		Use:          "download <remote-path> <local-dir>",
		Short:        "Download files from a PVC via temporary pod",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(cmd.Context(), &downloadRunOpts{
				configFlags: opts,
				streams:     streams,
				opts:        downloadOptions,
				args:        args,
			})
		},
	}
	cmd.Flags().StringVar(&downloadOptions.MountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	cmd.Flags().StringVar(&downloadOptions.PVC, "pvc", "", "PVC name (required)")
	opts.AddFlags(cmd.Flags())
	return cmd
}

func runDownload(ctx context.Context, opts *downloadRunOpts) error {
	namespace := "default"
	if opts.configFlags.Namespace != nil {
		namespace = *opts.configFlags.Namespace
	}
	return run(ctx,
		"download",
		opts.opts.PVC,
		namespace,
		opts.args[1],
		opts.args[0],
		opts.opts.MountPath,
	)
}
