package buildinfo

import (
	"runtime/debug"
	"strings"
)

var (
	Version = "0.1.0"
	BuildID = ""
)

func ResolvedBuildID() string {
	if value := strings.TrimSpace(BuildID); value != "" {
		return value
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	revision := ""
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if dirty {
		return revision + "-dirty"
	}
	return revision
}
