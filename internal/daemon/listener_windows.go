//go:build windows

package daemon

import (
	"net"

	"github.com/Microsoft/go-winio"
)

func ListenLocal(endpoint string) (net.Listener, error) {
	return winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;OW)",
		InputBufferSize:    64 * 1024, OutputBufferSize: 64 * 1024,
	})
}
