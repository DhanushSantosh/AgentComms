//go:build windows

package daemon

import (
	"net"

	"github.com/DhanushSantosh/AgentComms/internal/pipeacl"
	"github.com/Microsoft/go-winio"
)

func ListenLocal(endpoint string) (net.Listener, error) {
	descriptor, err := pipeacl.CurrentUserOnly()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(endpoint, &winio.PipeConfig{
		SecurityDescriptor: descriptor,
		InputBufferSize:    64 * 1024, OutputBufferSize: 64 * 1024,
	})
}
