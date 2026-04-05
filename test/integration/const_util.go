//go:build integration

package integration

import "time"

const (
	defaultTimeoutPerTest = 10 * time.Minute
	mountPathInContainer  = "/data"
	statePodName          = "state"
)
