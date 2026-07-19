//go:build !windows

package daemonclient

import (
	"context"
	"net"
)

func dialLocal(ctx context.Context, endpoint string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint)
}
