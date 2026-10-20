package scheduler

import (
	"fmt"
	"os"
)

// GetPodID returns a unique identifier for this pod/process
// Uses HOSTNAME environment variable (set by Kubernetes) and process ID
func GetPodID() string {
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
