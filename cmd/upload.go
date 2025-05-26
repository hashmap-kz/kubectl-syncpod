package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type UploadOptions struct {
	MountPath      string
	PVC            string
	Workers        int
	Dst            string
	Src            string
	AllowOverwrite bool
}

type uploadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        UploadOptions
}

func newUploadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	opts := genericclioptions.NewConfigFlags(true)
	uploadOptions := UploadOptions{}
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload local files to a PVC via temporary pod",
		Long: `
Example: 

Upload the local k8s directory to the container's /var/lib/postgresql/data/pgdata path, 
allowing overwriting of existing files:

kubectl syncpod upload \
  --namespace vault \
  --pvc postgresql \
  --mount-path=/var/lib/postgresql/data \
  --src=k8s \
  --dst=pgdata \
  --allow-overwrite
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpload(ctx, &uploadRunOpts{
				configFlags: opts,
				streams:     streams,
				opts:        uploadOptions,
			})
		},
	}
	cmd.Flags().StringVar(&uploadOptions.MountPath, "mount-path", "", "Mount path inside the helper pod (required)")
	cmd.Flags().StringVar(&uploadOptions.PVC, "pvc", "", "PVC name (required)")
	cmd.Flags().IntVarP(&uploadOptions.Workers, "workers", "w", 4, "Concurrent workers")
	cmd.Flags().StringVar(&uploadOptions.Src, "src", "", "Source")
	cmd.Flags().StringVar(&uploadOptions.Dst, "dst", "", "Destination")
	cmd.Flags().BoolVar(&uploadOptions.AllowOverwrite, "allow-overwrite", false, "Allow overwriting existing files")
	for _, rf := range []string{"mount-path", "pvc", "src", "dst"} {
		err := cmd.MarkFlagRequired(rf)
		if err != nil {
			log.Fatal(err)
		}
	}
	opts.AddFlags(cmd.Flags())
	return cmd
}

func runUpload(ctx context.Context, opts *uploadRunOpts) error {
	namespace := "default"
	if opts.configFlags.Namespace != nil {
		namespace = *opts.configFlags.Namespace
	}
	return run(ctx, &RunOpts{
		Mode:           "upload",
		PVC:            opts.opts.PVC,
		Namespace:      namespace,
		Remote:         opts.opts.Dst,
		Local:          opts.opts.Src,
		MountPath:      opts.opts.MountPath,
		Workers:        opts.opts.Workers,
		AllowOverwrite: opts.opts.AllowOverwrite,
	})
}
