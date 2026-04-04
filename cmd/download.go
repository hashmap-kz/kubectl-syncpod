package cmd

import (
	"context"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type downloadOpts struct {
	mountPath string
	pvc       string
	workers   int
	dst       string
	src       string
}

type downloadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	o           downloadOpts
}

func newDownloadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	downloadOptions := downloadOpts{}

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
				o:           downloadOptions,
			})
		},
	}

	cmd.Flags().IntVarP(&downloadOptions.workers, "workers", "w", 4, "Concurrent file workers")
	cmd.Flags().StringVar(&downloadOptions.mountPath, "mount-path", "", "Mount path inside helper pod")
	cmd.Flags().StringVar(&downloadOptions.pvc, "pvc", "", "PVC name")
	cmd.Flags().StringVar(&downloadOptions.src, "src", "", "Source path inside mount")
	cmd.Flags().StringVar(&downloadOptions.dst, "dst", "", "Local destination path")

	for _, rf := range []string{"mount-path", "pvc", "src", "dst"} {
		if err := cmd.MarkFlagRequired(rf); err != nil {
			log.Fatal(err)
		}
	}

	cfg.AddFlags(cmd.Flags())
	return cmd
}

func runDownload(ctx context.Context, runOpts *downloadRunOpts) error {
	namespace := resolveNamespace(runOpts.configFlags)
	return run(ctx, &RunOpts{
		Mode:      "download",
		PVC:       runOpts.o.pvc,
		Namespace: namespace,
		Remote:    runOpts.o.src,
		Local:     runOpts.o.dst,
		MountPath: runOpts.o.mountPath,
		Workers:   runOpts.o.workers,
		ObjName:   newObjName(),
	})
}
