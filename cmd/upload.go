package cmd

import (
	"context"
	"log"

	"github.com/hashmap-kz/kubectl-syncpod/internal/pipe"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func newUploadCmd(ctx context.Context, _ genericiooptions.IOStreams) *cobra.Command {
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
			uploadOptions.Namespace = pipe.ResolveNamespace(cfg)
			return pipe.Run(ctx, &pipe.RunOpts{
				Mode:           "upload",
				PVC:            uploadOptions.PVC,
				Namespace:      uploadOptions.Namespace,
				Local:          uploadOptions.Src,
				Remote:         uploadOptions.Dst,
				MountPath:      uploadOptions.MountPath,
				Workers:        uploadOptions.Workers,
				AllowOverwrite: uploadOptions.AllowOverwrite,
				Owner:          uploadOptions.Owner,
				ObjName:        pipe.NewObjName(),
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
