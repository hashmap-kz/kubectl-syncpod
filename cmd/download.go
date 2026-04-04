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

func newDownloadCmd(ctx context.Context, _ genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	downloadOptions := dto.DownloadOptions{}

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a PVC via temporary pod",
		Long: `
Examples:

kubectl syncpod download \
  --namespace vault \
  --pvc postgresql \
  --mount-path /var/lib/postgresql/data \
  --src pgdata \
  --dst backups
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			downloadOptions.Namespace = pipe.ResolveNamespace(cfg)
			return pipe.Run(ctx, &pipe.RunOpts{
				Mode:      "download",
				PVC:       downloadOptions.PVC,
				Namespace: downloadOptions.Namespace,
				Remote:    downloadOptions.Src,
				Local:     downloadOptions.Dst,
				MountPath: downloadOptions.MountPath,
				Workers:   downloadOptions.Workers,
				ObjName:   pipe.NewObjName(),
			})
		},
	}

	cmd.Flags().IntVarP(&downloadOptions.Workers, "workers", "w", 4, "Concurrent file workers")
	cmd.Flags().StringVar(&downloadOptions.MountPath, "mount-path", "", "Mount path inside helper pod")
	cmd.Flags().StringVar(&downloadOptions.PVC, "pvc", "", "PVC name")
	cmd.Flags().StringVar(&downloadOptions.Src, "src", "", "Source path inside mount")
	cmd.Flags().StringVar(&downloadOptions.Dst, "dst", "", "Local destination path")

	for _, rf := range []string{"mount-path", "pvc", "src", "dst"} {
		if err := cmd.MarkFlagRequired(rf); err != nil {
			log.Fatal(err)
		}
	}

	cfg.AddFlags(cmd.Flags())
	return cmd
}
