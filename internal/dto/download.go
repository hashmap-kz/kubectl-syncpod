package dto

type DownloadOptions struct {
	Namespace string
	MountPath string
	PVC       string
	Workers   int
	Dst       string
	Src       string
}

type DownloadSTSOptions struct {
	Namespace     string
	Dst           string
	VolumeWorkers int
	FileWorkers   int
	StsName       string
}
