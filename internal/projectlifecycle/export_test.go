//go:build !js

package projectlifecycle

import "context"

// Test-only access to the RFC 0041 snapshot rebuild for the external tests,
// which need a real signed project from internal/testsupport.
func RebuildPersonalSnapshots(ctx context.Context, path string) error {
	return rebuildSnapshotsFromEvents(ctx, path, personalAuthoritySnapshots)
}

func RebuildCacheSnapshots(ctx context.Context, path string) error {
	return rebuildSnapshotsFromEvents(ctx, path, projectionCacheSnapshots)
}
