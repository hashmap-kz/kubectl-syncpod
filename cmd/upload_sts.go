package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type uploadSTSOptions struct {
	Namespace      string
	Src            string
	VolumeWorkers  int
	FileWorkers    int
	AllowOverwrite bool
	Owner          string
	SkipMissing    bool
}

type uploadSTSRunOpts struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericiooptions.IOStreams
	opts        uploadSTSOptions
}

func newUploadSTSCmd(ctx context.Context, streams genericiooptions.IOStreams) *cobra.Command {
	cfg := genericclioptions.NewConfigFlags(true)
	opts := uploadSTSOptions{}

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
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Namespace = resolveNamespace(cfg)
			return runUploadSTS(ctx, args[0], &uploadSTSRunOpts{
				configFlags: cfg,
				streams:     streams,
				opts:        opts,
			})
		},
	}

	cmd.Flags().StringVar(&opts.Src, "src", "", "Local source root, e.g. ./backup")
	cmd.Flags().IntVar(&opts.VolumeWorkers, "volume-workers", 2, "Concurrent PVC upload jobs")
	cmd.Flags().IntVar(&opts.FileWorkers, "file-workers", 2, "Concurrent file workers per PVC")
	cmd.Flags().BoolVar(&opts.AllowOverwrite, "allow-overwrite", false, "Allow overwrite of existing target volume contents")
	cmd.Flags().StringVar(&opts.Owner, "owner", "", "Optional owner (uid:gid or user:group)")
	cmd.Flags().BoolVar(&opts.SkipMissing, "skip-missing", false, "Skip missing local pod/volume directories instead of failing")

	//nolint:errcheck
	_ = cmd.MarkFlagRequired("src")
	cfg.AddFlags(cmd.Flags())

	return cmd
}

func runUploadSTS(ctx context.Context, stsName string, ropts *uploadSTSRunOpts) error {
	manifestPath := filepath.Join(ropts.opts.Src, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return runUploadSTSFromManifest(
			ctx,
			ropts.opts.Namespace,
			stsName,
			ropts.opts.Src,
			ropts.opts.VolumeWorkers,
			ropts.opts.FileWorkers,
			ropts.opts.AllowOverwrite,
			ropts.opts.Owner,
			ropts.opts.SkipMissing,
		)
	}
	return fmt.Errorf("cannot upload as no manifest given")
}

// restore sts

func runUploadSTSFromManifest(
	ctx context.Context,
	namespace, stsName, srcRoot string,
	volumeWorkers, fileWorkers int,
	allowOverwrite bool,
	owner string,
	skipMissing bool,
) error {
	manifestPath := filepath.Join(srcRoot, "manifest.json")

	manifest, err := kub.ReadStatefulSetBackupManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	if manifest.StatefulSet != stsName {
		return fmt.Errorf(
			"manifest statefulset mismatch: manifest=%q requested=%q",
			manifest.StatefulSet, stsName,
		)
	}

	sources, err := ValidateManifestSources(srcRoot, manifest, skipMissing)
	if err != nil {
		return fmt.Errorf("validate manifest sources: %w", err)
	}

	type result struct {
		src RestoreSource
		err error
	}

	jobs := make(chan RestoreSource)
	results := make(chan result, len(sources))

	var wg sync.WaitGroup
	for i := 0; i < volumeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for src := range jobs {
				err := run(ctx, &RunOpts{
					Mode:           "upload",
					PVC:            src.Entry.PVCName,
					Namespace:      namespace,
					Local:          src.LocalSrc,
					Remote:         ".",
					MountPath:      src.Entry.MountPath,
					Workers:        fileWorkers,
					AllowOverwrite: allowOverwrite,
					Owner:          owner,
					ObjName:        newObjName(),
				})

				results <- result{
					src: src,
					err: err,
				}
			}
		}()
	}

	go func() {
		//nolint:gocritic
		for _, src := range sources {
			jobs <- src
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf(
				"%s/%s (%s <- %s): %w",
				r.src.Entry.PodName,
				r.src.Entry.VolumeName,
				r.src.Entry.PVCName,
				r.src.LocalSrc,
				r.err,
			))
		}
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

// TODO:feat/sts-vols-discover-1 - simplify CLI

// restore sts

type RestoreTarget struct {
	Manifest kub.StatefulSetVolume
	Current  kub.PodVolume
	LocalSrc string
	Warnings []string
}

type RestoreSource struct {
	Entry    kub.StatefulSetVolume
	LocalSrc string
}

func ValidateManifestSources(
	srcRoot string,
	manifest *kub.StatefulSetBackupManifest,
	skipMissing bool,
) ([]RestoreSource, error) {
	var result []RestoreSource
	var errs []error

	for _, entry := range manifest.Entries {
		localSrc := filepath.Join(srcRoot, filepath.FromSlash(entry.LocalPath))

		info, err := os.Stat(localSrc)
		if err != nil {
			if os.IsNotExist(err) && skipMissing {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"stat local source for pod=%q volume=%q path=%q: %w",
				entry.PodName, entry.VolumeName, localSrc, err,
			))
			continue
		}

		if !info.IsDir() {
			errs = append(errs, fmt.Errorf(
				"local source for pod=%q volume=%q is not a directory: %q",
				entry.PodName, entry.VolumeName, localSrc,
			))
			continue
		}

		if entry.PVCName == "" {
			errs = append(errs, fmt.Errorf(
				"manifest entry for pod=%q volume=%q has empty pvc_name",
				entry.PodName, entry.VolumeName,
			))
			continue
		}

		if entry.MountPath == "" {
			errs = append(errs, fmt.Errorf(
				"manifest entry for pod=%q volume=%q has empty mount_path",
				entry.PodName, entry.VolumeName,
			))
			continue
		}

		result = append(result, RestoreSource{
			Entry:    entry,
			LocalSrc: localSrc,
		})
	}

	if len(errs) > 0 {
		return nil, joinErrors(errs)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Entry.Ordinal != result[j].Entry.Ordinal {
			return result[i].Entry.Ordinal < result[j].Entry.Ordinal
		}
		return result[i].Entry.VolumeName < result[j].Entry.VolumeName
	})

	return result, nil
}
