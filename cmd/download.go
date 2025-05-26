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
	Workers   int
	Dst       string
	Src       string
}

type downloadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        DownloadOptions
}

func newDownloadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	opts := genericclioptions.NewConfigFlags(true)
	downloadOptions := DownloadOptions{}
	cmd := &cobra.Command{
		Use:           "download",
		Short:         "Download files from a PVC via temporary pod",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDownload(ctx, &downloadRunOpts{
				configFlags: opts,
				streams:     streams,
				opts:        downloadOptions,
			})
		},
	}
	cmd.Flags().IntVarP(&downloadOptions.Workers, "workers", "w", 4, "Concurrent workers")
	cmd.Flags().StringVar(&downloadOptions.MountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	cmd.Flags().StringVar(&downloadOptions.PVC, "pvc", "", "PVC name (required)")
	cmd.Flags().StringVar(&downloadOptions.Src, "src", "", "Source")
	cmd.Flags().StringVar(&downloadOptions.Dst, "dst", "", "Destination")
	for _, rf := range []string{"mount-path", "pvc", "src", "dst"} {
		_ = cmd.MarkFlagRequired(rf)
	}
	opts.AddFlags(cmd.Flags())
	return cmd
}

func runDownload(ctx context.Context, opts *downloadRunOpts) error {
	namespace := "default"
	if opts.configFlags.Namespace != nil {
		namespace = *opts.configFlags.Namespace
	}
	return run(ctx, &RunOpts{
		Mode:      "download",
		PVC:       opts.opts.PVC,
		Namespace: namespace,
		Remote:    opts.opts.Src,
		Local:     opts.opts.Dst,
		MountPath: opts.opts.MountPath,
		Workers:   opts.opts.Workers,
	})
}
