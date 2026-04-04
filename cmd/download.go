package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type DownloadOptions struct {
	MountPath string
	PVC       string
	Workers   int
	Dst       string
	Src       string
}

type downloadRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        DownloadOptions
}

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

func newDownloadCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	downloadOptions := DownloadOptions{}

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a PVC via temporary pod",
		Long: `
Examples:

Download a directory from a single PVC:

kubectl syncpod download \
  --namespace vault \
  --pvc postgresql \
  --mount-path /var/lib/postgresql/data \
  --src pgdata \
  --dst backups

Download all PVC-backed volumes of a StatefulSet:

kubectl syncpod download sts rabbitmq \
  --namespace mq \
  --dst ./backup
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDownload(ctx, &downloadRunOpts{
				configFlags: cfg,
				streams:     streams,
				opts:        downloadOptions,
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
	cmd.AddCommand(newDownloadSTSCmd(ctx, streams))

	return cmd
}

func newDownloadSTSCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	opts := downloadSTSOptions{}

	cmd := &cobra.Command{
		Use:   "sts <name>",
		Short: "Download all PVC-backed volumes of a StatefulSet",
		Args:  cobra.ExactArgs(1),
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
	cmd.Flags().IntVar(&opts.FileWorkers, "file-workers", 4, "Concurrent file workers per PVC")
	//nolint:errcheck
	_ = cmd.MarkFlagRequired("dst")

	cfg.AddFlags(cmd.Flags())
	return cmd
}

func runDownload(ctx context.Context, opts *downloadRunOpts) error {
	namespace := resolveNamespace(opts.configFlags)
	return run(ctx, &RunOpts{
		Mode:      "download",
		PVC:       opts.opts.PVC,
		Namespace: namespace,
		Remote:    opts.opts.Src,
		Local:     opts.opts.Dst,
		MountPath: opts.opts.MountPath,
		Workers:   opts.opts.Workers,
		ObjName:   newObjName(),
	})
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
