package cmd

import (
	"context"
	"log"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type uploadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	o           *dto.UploadOptions
}

func newUploadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	uploadOptions := dto.UploadOptions{}

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
				o:           &uploadOptions,
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

func runUpload(ctx context.Context, runOpts *uploadRunOpts) error {
	return run(ctx, &RunOpts{
		Mode:           "upload",
		PVC:            runOpts.o.PVC,
		Namespace:      resolveNamespace(runOpts.configFlags),
		Local:          runOpts.o.Src,
		Remote:         runOpts.o.Dst,
		MountPath:      runOpts.o.MountPath,
		Workers:        runOpts.o.Workers,
		AllowOverwrite: runOpts.o.AllowOverwrite,
		Owner:          runOpts.o.Owner,
		ObjName:        newObjName(),
	})
}
