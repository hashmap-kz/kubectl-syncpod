package pipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/kubectl-syncpod/internal/dto"
	"github.com/hashmap-kz/kubectl-syncpod/internal/kub"
)

func RunDownloadSTS(ctx context.Context, runOpts *dto.DownloadSTSOpts) error {
	_, client, err := initConfigAndClient()
	if err != nil {
		return err
	}

	vols, err := kub.DiscoverStatefulSetPVCs(ctx, client, runOpts.Namespace, runOpts.StsName)
	if err != nil {
		return err
	}
	if len(vols) == 0 {
		return fmt.Errorf("no PVC-backed volumes found for StatefulSet %q", runOpts.StsName)
	}

	if err := os.MkdirAll(runOpts.Dst, 0o755); err != nil {
		return fmt.Errorf("create destination root: %w", err)
	}

	type result struct {
		vol kub.PodVolume
		err error
	}

	jobs := make(chan kub.PodVolume)
	results := make(chan result, len(vols))

	var wg sync.WaitGroup
	for i := 0; i < runOpts.VolumeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vol := range jobs {
				localDst := filepath.Join(runOpts.Dst, vol.PodName, vol.VolumeName)

				err := Run(ctx, &dto.RunOpts{
					Mode:      "download",
					PVC:       vol.PVCName,
					Namespace: runOpts.Namespace,
					Remote:    ".",
					Local:     localDst,
					MountPath: vol.MountPath,
					Workers:   runOpts.FileWorkers,
					ObjName:   kub.NewObjName(),
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

	manifest := kub.BuildStatefulSetBackupManifest(runOpts.Namespace, runOpts.StsName, vols)
	err = kub.WriteStatefulSetBackupManifest(filepath.Join(runOpts.Dst, "manifest.json"), manifest)

	return err
}
