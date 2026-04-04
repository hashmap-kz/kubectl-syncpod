package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type downloadSTSRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	o           *dto.DownloadSTSOptions
}

func newDownloadSTSCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	downloadSTSOptions := dto.DownloadSTSOptions{}

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
			downloadSTSOptions.Namespace = resolveNamespace(cfg)
			downloadSTSOptions.StsName = args[0]
			return runDownloadSTS(ctx, &downloadSTSRunOpts{
				configFlags: cfg,
				streams:     streams,
				o:           &downloadSTSOptions,
			})
		},
	}

	cmd.Flags().StringVar(&downloadSTSOptions.Dst, "dst", "", "Local destination root")
	cmd.Flags().IntVar(&downloadSTSOptions.VolumeWorkers, "volume-workers", 2, "Concurrent PVC download jobs")
	cmd.Flags().IntVar(&downloadSTSOptions.FileWorkers, "file-workers", 2, "Concurrent file workers per PVC")
	//nolint:errcheck
	_ = cmd.MarkFlagRequired("dst")

	cfg.AddFlags(cmd.Flags())
	return cmd
}

func runDownloadSTS(ctx context.Context, runOpts *downloadSTSRunOpts) error {
	_, client, err := initConfigAndClient()
	if err != nil {
		return err
	}

	vols, err := kub.DiscoverStatefulSetPVCs(ctx, client, runOpts.o.Namespace, runOpts.o.StsName)
	if err != nil {
		return err
	}
	if len(vols) == 0 {
		return fmt.Errorf("no PVC-backed volumes found for StatefulSet %q", runOpts.o.StsName)
	}

	if err := os.MkdirAll(runOpts.o.Dst, 0o755); err != nil {
		return fmt.Errorf("create destination root: %w", err)
	}

	type result struct {
		vol kub.PodVolume
		err error
	}

	jobs := make(chan kub.PodVolume)
	results := make(chan result, len(vols))

	var wg sync.WaitGroup
	for i := 0; i < runOpts.o.VolumeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vol := range jobs {
				localDst := filepath.Join(runOpts.o.Dst, vol.PodName, vol.VolumeName)

				err := run(ctx, &RunOpts{
					Mode:      "download",
					PVC:       vol.PVCName,
					Namespace: runOpts.o.Namespace,
					Remote:    ".",
					Local:     localDst,
					MountPath: vol.MountPath,
					Workers:   runOpts.o.FileWorkers,
					ObjName:   newObjName(),
				})

				results <- result{vol: vol, err: err}
			}
		}()
	}

	go func() {
		for _, vol := range vols {
			jobs <- vol
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s/%s (%s -> %s): %w",
				r.vol.PodName, r.vol.VolumeName, r.vol.PVCName, r.vol.MountPath, r.err))
		}
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	manifest := kub.BuildStatefulSetBackupManifest(runOpts.o.Namespace, runOpts.o.StsName, vols)
	err = kub.WriteStatefulSetBackupManifest(filepath.Join(runOpts.o.Dst, "manifest.json"), manifest)

	return err
}
