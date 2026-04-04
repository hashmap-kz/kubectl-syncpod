package cmd

import (
	"context"
	"log"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type downloadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	o           *dto.DownloadOptions
}

func newDownloadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
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
			return runDownload(ctx, &downloadRunOpts{
				configFlags: cfg,
				streams:     streams,
				o:           &downloadOptions,
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

func runDownload(ctx context.Context, opts *downloadRunOpts) error {
	return run(ctx, &RunOpts{
		Mode:      "download",
		PVC:       opts.o.PVC,
		Namespace: resolveNamespace(opts.configFlags),
		Remote:    opts.o.Src,
		Local:     opts.o.Dst,
		MountPath: opts.o.MountPath,
		Workers:   opts.o.Workers,
		ObjName:   newObjName(),
	})
}
