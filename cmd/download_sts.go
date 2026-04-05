package cmd

import (
	"context"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"
	"github.com/hashmap-kz/kubectl-syncpod/internal/pipe"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func newDownloadSTSCmd(ctx context.Context, cfg *genericclioptions.ConfigFlags, _ genericiooptions.IOStreams) *cobra.Command {
	downloadSTSOptions := dto.DownloadSTSOpts{}

	cmd := &cobra.Command{
		Use:   "download-sts",
		Short: "Download all PVC-backed volumes of a StatefulSet",
		Long: `
Examples:

kubectl syncpod download-sts rabbitmq \
  --namespace mq \
  --dst ./backup
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			downloadSTSOptions.Namespace = kub.ResolveNamespace(cfg)
			downloadSTSOptions.StsName = args[0]
			return pipe.RunDownloadSTS(ctx, &downloadSTSOptions)
		},
	}

	cmd.Flags().StringVar(&downloadSTSOptions.Dst, "dst", "", "Local destination root")
	cmd.Flags().IntVar(&downloadSTSOptions.VolumeWorkers, "volume-workers", 2, "Concurrent PVC download jobs")
	cmd.Flags().IntVar(&downloadSTSOptions.FileWorkers, "file-workers", 2, "Concurrent file workers per PVC")
	//nolint:errcheck
	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
