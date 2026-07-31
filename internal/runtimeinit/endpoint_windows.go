//go:build windows

package runtimeinit

func DaemonEndpoint(_ string, projectID string) string {
	return `\\.\pipe\agent-comms-` + projectID
}
