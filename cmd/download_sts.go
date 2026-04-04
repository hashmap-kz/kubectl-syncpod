package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type downloadSTSOptions struct {
	Namespace     string
	Dst           string
	VolumeWorkers int
	FileWorkers   int
}

type downloadSTSRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        downloadSTSOptions
}

func newDownloadSTSCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	opts := downloadSTSOptions{}

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
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Namespace = resolveNamespace(cfg)
			return runDownloadSTS(ctx, args[0], &downloadSTSRunOpts{
				configFlags: cfg,
				streams:     streams,
				opts:        opts,
			})
		},
	}

	cmd.Flags().StringVar(&opts.Dst, "dst", "", "Local destination root")
	cmd.Flags().IntVar(&opts.VolumeWorkers, "volume-workers", 2, "Concurrent PVC download jobs")
	cmd.Flags().IntVar(&opts.FileWorkers, "file-workers", 2, "Concurrent file workers per PVC")
	//nolint:errcheck
	_ = cmd.MarkFlagRequired("dst")

	cfg.AddFlags(cmd.Flags())
	return cmd
}

func runDownloadSTS(ctx context.Context, stsName string, ropts *downloadSTSRunOpts) error {
	_, client, err := initConfigAndClient()
	if err != nil {
		return err
	}

	vols, err := kub.DiscoverStatefulSetPVCs(ctx, client, ropts.opts.Namespace, stsName)
	if err != nil {
		return err
	}
	if len(vols) == 0 {
		return fmt.Errorf("no PVC-backed volumes found for StatefulSet %q", stsName)
	}

	if err := os.MkdirAll(ropts.opts.Dst, 0o755); err != nil {
		return fmt.Errorf("create destination root: %w", err)
	}

	type result struct {
		vol kub.PodVolume
		err error
	}

	jobs := make(chan kub.PodVolume)
	results := make(chan result, len(vols))

	var wg sync.WaitGroup
	for i := 0; i < ropts.opts.VolumeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vol := range jobs {
				localDst := filepath.Join(ropts.opts.Dst, vol.PodName, vol.VolumeName)

				err := run(ctx, &RunOpts{
					Mode:      "download",
					PVC:       vol.PVCName,
					Namespace: ropts.opts.Namespace,
					Remote:    ".",
					Local:     localDst,
					MountPath: vol.MountPath,
					Workers:   ropts.opts.FileWorkers,
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

	manifest := kub.BuildStatefulSetBackupManifest(ropts.opts.Namespace, stsName, vols)
	err = kub.WriteStatefulSetBackupManifest(filepath.Join(ropts.opts.Dst, "manifest.json"), manifest)

	return err
}
