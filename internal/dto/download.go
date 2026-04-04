package dto

type DownloadOpts struct {
	Namespace string
	MountPath string
	PVC       string
	Workers   int
	Dst       string
	Src       string
}

type DownloadSTSOpts struct {
	Namespace     string
	Dst           string
	VolumeWorkers int
	FileWorkers   int
	StsName       string
}
