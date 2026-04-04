package dto

type UploadOpts struct {
	Namespace      string
	MountPath      string
	PVC            string
	Workers        int
	Src            string
	Dst            string
	AllowOverwrite bool
	Owner          string
}

type UploadSTSOpts struct {
	Namespace      string
	Src            string
	VolumeWorkers  int
	FileWorkers    int
	AllowOverwrite bool
	Owner          string
	SkipMissing    bool
	StsName        string
}
