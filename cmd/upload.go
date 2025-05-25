package cmd

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type UploadOptions struct {
	MountPath string
	PVC       string
}

type uploadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        UploadOptions
	args        []string
}

func newUploadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	opts := genericclioptions.NewConfigFlags(true)
	uploadOptions := UploadOptions{}
	cmd := &cobra.Command{
		Use:          "upload <local-dir> <remote-path>",
		Short:        "Upload local files to a PVC via temporary pod",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpload(ctx, &uploadRunOpts{
				configFlags: opts,
				streams:     streams,
				opts:        uploadOptions,
				args:        args,
			})
		},
	}
	cmd.Flags().StringVar(&uploadOptions.MountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	cmd.Flags().StringVar(&uploadOptions.PVC, "pvc", "", "PVC name (required)")
	opts.AddFlags(cmd.Flags())
	return cmd
}

func runUpload(ctx context.Context, opts *uploadRunOpts) error {
	namespace := "default"
	if opts.configFlags.Namespace != nil {
		namespace = *opts.configFlags.Namespace
	}
	return run(ctx,
		"upload",
		opts.opts.PVC,
		namespace,
		opts.args[1],
		opts.args[0],
		opts.opts.MountPath,
	)
}
