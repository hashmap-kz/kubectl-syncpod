package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type uploadOptions struct {
	MountPath      string
	PVC            string
	Workers        int
	Src            string
	Dst            string
	AllowOverwrite bool
	Owner          string
}

type uploadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        uploadOptions
}

func newUploadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	uploadOptions := uploadOptions{}

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload files to a PVC via temporary pod",
		Long: `
Examples:

kubectl syncpod upload \
  --namespace mq \
  --pvc data-rabbitmq-0 \
  --mount-path /var/lib/rabbitmq \
  --src ./restore \
  --dst .
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpload(ctx, &uploadRunOpts{
				configFlags: cfg,
				streams:     streams,
				opts:        uploadOptions,
			})
		},
	}

	cmd.Flags().IntVarP(&uploadOptions.Workers, "workers", "w", 4, "Concurrent file workers")
	cmd.Flags().StringVar(&uploadOptions.MountPath, "mount-path", "", "Mount path inside helper pod")
	cmd.Flags().StringVar(&uploadOptions.PVC, "pvc", "", "PVC name")
	cmd.Flags().StringVar(&uploadOptions.Src, "src", "", "Local source path")
	cmd.Flags().StringVar(&uploadOptions.Dst, "dst", "", "Destination path inside mount")
	cmd.Flags().BoolVar(&uploadOptions.AllowOverwrite, "allow-overwrite", false, "Allow overwrite of existing destination")
	cmd.Flags().StringVar(&uploadOptions.Owner, "owner", "", "Optional owner (uid:gid or user:group)")

	for _, rf := range []string{"mount-path", "pvc", "src", "dst"} {
		if err := cmd.MarkFlagRequired(rf); err != nil {
			log.Fatal(err)
		}
	}

	cfg.AddFlags(cmd.Flags())
	return cmd
}

func runUpload(ctx context.Context, opts *uploadRunOpts) error {
	namespace := resolveNamespace(opts.configFlags)
	return run(ctx, &RunOpts{
		Mode:           "upload",
		PVC:            opts.opts.PVC,
		Namespace:      namespace,
		Local:          opts.opts.Src,
		Remote:         opts.opts.Dst,
		MountPath:      opts.opts.MountPath,
		Workers:        opts.opts.Workers,
		AllowOverwrite: opts.opts.AllowOverwrite,
		Owner:          opts.opts.Owner,
		ObjName:        newObjName(),
	})
}
