package failure

import (
	"errors"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const CodeExternal = "EXTERNAL"

func Code(err error) string {
	var controlError *controlplane.Error
	if errors.As(err, &controlError) {
		return string(controlError.Code)
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "credential") ||
		strings.Contains(message, "active principal") ||
		strings.Contains(message, "role required") ||
		strings.Contains(message, "human principal"):
		return string(controlplane.CodeAuthorization)
	case strings.Contains(message, "signature") ||
		strings.Contains(message, "hash") ||
		strings.Contains(message, "chain"):
		return string(controlplane.CodeIntegrity)
	case strings.Contains(message, "remote"):
		return CodeExternal
	default:
		return string(controlplane.CodeValidation)
	}
}

func ExitStatus(err error) int {
	switch Code(err) {
	case string(controlplane.CodeValidation):
		return 2
	case string(controlplane.CodeAuthorization):
		return 3
	case string(controlplane.CodeIntegrity):
		return 5
	case CodeExternal:
		return 7
	case string(controlplane.CodeOffline), string(controlplane.CodeUnavailable):
		return 8
	case string(controlplane.CodeConflict), string(controlplane.CodeStalePrecondition):
		return 9
	case string(controlplane.CodeRateLimited):
		return 10
	default:
		return 1
	}
}
