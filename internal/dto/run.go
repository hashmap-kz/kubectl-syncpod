package dto

type RunOpts struct {
	Mode           string
	PVC            string
	Namespace      string
	Remote         string
	Local          string
	MountPath      string
	Workers        int
	AllowOverwrite bool
	Owner          string
	ObjName        string
}
