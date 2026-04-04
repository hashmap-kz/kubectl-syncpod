package pipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"
	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"
)

type restoreSource struct {
	entry    kub.StatefulSetVolume
	localSrc string
}

func RunUploadSTS(ctx context.Context, ropts *dto.UploadSTSOpts) error {
	manifestPath := filepath.Join(ropts.Src, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return runUploadSTSFromManifest(ctx, ropts)
	}
	return fmt.Errorf("cannot upload as no manifest given")
}

func runUploadSTSFromManifest(ctx context.Context, d *dto.UploadSTSOpts) error {
	manifestPath := filepath.Join(d.Src, "manifest.json")

	manifest, err := kub.ReadStatefulSetBackupManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	if manifest.StatefulSet != d.StsName {
		return fmt.Errorf(
			"manifest statefulset mismatch: manifest=%q requested=%q",
			manifest.StatefulSet, d.StsName,
		)
	}

	sources, err := validateManifestSources(d.Src, manifest, d.SkipMissing)
	if err != nil {
		return fmt.Errorf("validate manifest sources: %w", err)
	}

	type result struct {
		src restoreSource
		err error
	}

	jobs := make(chan restoreSource)
	results := make(chan result, len(sources))

	var wg sync.WaitGroup
	for i := 0; i < d.VolumeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for src := range jobs {
				err := Run(ctx, &dto.RunOpts{
					Mode:           "upload",
					PVC:            src.entry.PVCName,
					Namespace:      d.Namespace,
					Local:          src.localSrc,
					Remote:         ".",
					MountPath:      src.entry.MountPath,
					Workers:        d.FileWorkers,
					AllowOverwrite: d.AllowOverwrite,
					Owner:          d.Owner,
					ObjName:        kub.NewObjName(),
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
				r.src.entry.PodName,
				r.src.entry.VolumeName,
				r.src.entry.PVCName,
				r.src.localSrc,
				r.err,
			))
		}
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

func validateManifestSources(
	srcRoot string,
	manifest *kub.StatefulSetBackupManifest,
	skipMissing bool,
) ([]restoreSource, error) {
	var result []restoreSource
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

		result = append(result, restoreSource{
			entry:    entry,
			localSrc: localSrc,
		})
	}

	if len(errs) > 0 {
		return nil, joinErrors(errs)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].entry.Ordinal != result[j].entry.Ordinal {
			return result[i].entry.Ordinal < result[j].entry.Ordinal
		}
		return result[i].entry.VolumeName < result[j].entry.VolumeName
	})

	return result, nil
}
