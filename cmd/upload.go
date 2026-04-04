package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type uploadOpts struct {
	mountPath      string
	pvc            string
	workers        int
	src            string
	dst            string
	allowOverwrite bool
	owner          string
}

type uploadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	o           uploadOpts
}

func newUploadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	uploadOptions := uploadOpts{}

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
				o:           uploadOptions,
			})
		},
	}

	cmd.Flags().IntVarP(&uploadOptions.workers, "workers", "w", 4, "Concurrent file workers")
	cmd.Flags().StringVar(&uploadOptions.mountPath, "mount-path", "", "Mount path inside helper pod")
	cmd.Flags().StringVar(&uploadOptions.pvc, "pvc", "", "PVC name")
	cmd.Flags().StringVar(&uploadOptions.src, "src", "", "Local source path")
	cmd.Flags().StringVar(&uploadOptions.dst, "dst", "", "Destination path inside mount")
	cmd.Flags().BoolVar(&uploadOptions.allowOverwrite, "allow-overwrite", false, "Allow overwrite of existing destination")
	cmd.Flags().StringVar(&uploadOptions.owner, "owner", "", "Optional owner (uid:gid or user:group)")

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
		PVC:            opts.o.pvc,
		Namespace:      namespace,
		Local:          opts.o.src,
		Remote:         opts.o.dst,
		MountPath:      opts.o.mountPath,
		Workers:        opts.o.workers,
		AllowOverwrite: opts.o.allowOverwrite,
		Owner:          opts.o.owner,
		ObjName:        newObjName(),
	})
}
