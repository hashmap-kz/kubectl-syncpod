package cmd

import (
	"context"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"
	"github.com/hashmap-kz/kubectl-syncpod/internal/pipe"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func newUploadSTSCmd(ctx context.Context, _ genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	uploadSTSOptions := dto.UploadSTSOptions{}

	cmd := &cobra.Command{
		Use:   "upload-sts",
		Short: "Upload a StatefulSet-shaped backup into all PVC-backed volumes",
		Long: `
Examples:

kubectl syncpod upload-sts rabbitmq \
  --namespace mq \
  --src ./backup
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			uploadSTSOptions.Namespace = pipe.ResolveNamespace(cfg)
			uploadSTSOptions.StsName = args[0]
			return pipe.RunUploadSTS(ctx, &uploadSTSOptions)
		},
	}

	cmd.Flags().StringVar(&uploadSTSOptions.Src, "src", "", "Local source root, e.g. ./backup")
	cmd.Flags().IntVar(&uploadSTSOptions.VolumeWorkers, "volume-workers", 2, "Concurrent PVC upload jobs")
	cmd.Flags().IntVar(&uploadSTSOptions.FileWorkers, "file-workers", 2, "Concurrent file workers per PVC")
	cmd.Flags().BoolVar(&uploadSTSOptions.AllowOverwrite, "allow-overwrite", false, "Allow overwrite of existing target volume contents")
	cmd.Flags().StringVar(&uploadSTSOptions.Owner, "owner", "", "Optional owner (uid:gid or user:group)")
	cmd.Flags().BoolVar(&uploadSTSOptions.SkipMissing, "skip-missing", false, "Skip missing local pod/volume directories instead of failing")

	//nolint:errcheck
	_ = cmd.MarkFlagRequired("src")
	cfg.AddFlags(cmd.Flags())

	return cmd
}
