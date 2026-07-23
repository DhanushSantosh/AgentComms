package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/acpclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	acpsdk "github.com/coder/acp-go-sdk"
)

// Adapter captures everything about a specific AI provider that Worker's
// execution loop must not know: how to validate provider-specific Config
// fields, and how to run one invocation to completion. Exec-based adapters
// (claude, codex) run a provider CLI as a subprocess; ACP-based adapters
// (claude-acp, and future OpenCode/Codex-over-ACP adapters) speak the Agent
// Client Protocol to a provider process instead. Worker never branches on
// which kind it's talking to.
type Adapter interface {
	// Validate normalizes and checks the fields of Config this adapter
	// owns, filling in defaults. It runs after the adapter-agnostic checks
	// in validateConfig.
	Validate(config *Config) error
	// Execute runs invocation to completion and returns the agent's final
	// textual result.
	Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error)
}

// cliAdapter is implemented by adapters that run a provider CLI as a
// subprocess and capture its stdout — the shape runCLIAdapter drives.
type cliAdapter interface {
	// Arguments builds the CLI argv used to invoke the provider.
	Arguments(config Config) []string
	// Prompt builds the task prompt delivered to the provider on stdin.
	Prompt(actor string, invocation model.Invocation) string
}

// runCLIAdapter execs config.Executable with the adapter's argv, feeding it
// the adapter's prompt on stdin and capturing bounded stdout/stderr. It is
// the shared execution path for every cliAdapter (claude, codex).
func runCLIAdapter(ctx context.Context, config Config, adapter cliAdapter, invocation model.Invocation) (string, error) {
	prompt := adapter.Prompt(config.Actor, invocation)
	arguments := adapter.Arguments(config)
	command := exec.CommandContext(ctx, config.Executable, arguments...)
	command.Dir = config.WorkDir
	command.Env = os.Environ()
	command.Stdin = strings.NewReader(prompt)
	stdout := &boundedBuffer{limit: maxAgentOutputBytes}
	stderr := &boundedBuffer{limit: maxAgentOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("execution deadline reached: %w", ctx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", errors.New(truncateUTF8(detail, maxFailureReasonBytes))
	}
	if stdout.truncated {
		return "", fmt.Errorf("agent output exceeded %d bytes", maxAgentOutputBytes)
	}
	return stdout.String(), nil
}

// acpResult interprets one ACP Prompt call's outcome the same way for every
// ACP-based adapter (claude-acp, opencode-acp, codex-acp): a refusal or
// unexpected stop reason is an error, output exceeding the configured byte
// bound is an error, and — the case a bare stop-reason switch would miss —
// empty output after a permission request was denied is treated as a
// failure rather than a vacuous success. Confirmed live: an ACP agent whose
// shell command gets denied by acpclient's deny-by-default governance can
// simply produce no final text at all rather than explaining it couldn't
// proceed; without this check that surfaced as a COMPLETED invocation with
// a meaningless placeholder result instead of routing to WAITING the way a
// failed exec-based adapter already does.
func acpResult(output string, stopReason acpsdk.StopReason, session *acpclient.Session) (string, error) {
	switch stopReason {
	case acpsdk.StopReasonEndTurn, acpsdk.StopReasonMaxTokens, acpsdk.StopReasonMaxTurnRequests:
		if session.Truncated() {
			return "", fmt.Errorf("agent output exceeded %d bytes", maxAgentOutputBytes)
		}
		if strings.TrimSpace(output) == "" && session.Denied() {
			return "", fmt.Errorf("agent produced no result after a permission request was denied for: %s",
				strings.Join(session.DeniedKinds(), ", "))
		}
		return output, nil
	case acpsdk.StopReasonRefusal:
		return "", errors.New("agent refused the invocation")
	default:
		return "", fmt.Errorf("agent stopped with reason %q before completing the invocation", stopReason)
	}
}

// validateExecutablePath checks that path is an absolute, executable file.
// It is shared by every cliAdapter's Validate, since only cliAdapter
// implementations actually exec a local binary — ACP-based adapters spawn
// their agent process by other means and don't require this check.
func validateExecutablePath(path, label string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s must use an absolute path", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		return fmt.Errorf("%s is not executable", label)
	}
	return nil
}

var adapters = map[string]Adapter{
	"claude":        claudeAdapter{},
	"codex":         codexAdapter{},
	"claude-acp":    claudeACPAdapter{},
	"opencode-acp":  openCodeACPAdapter{},
	"codex-acp":     codexACPAdapter{},
	"opencode-live": openCodeLiveAdapter{},
	"claude-live":   claudeLiveAdapter{},
	"codex-live":    codexLiveAdapter{},
}

// RequiresExecutable reports whether the named adapter execs a local CLI
// binary and therefore needs a resolved Config.Executable path. ACP-based
// adapters spawn their agent process by other means (e.g. npx) and don't.
// Callers building worker.Config from CLI flags use this to decide whether
// to look up an executable at all, rather than assuming every adapter needs
// one the way the exec-based adapters historically did.
func RequiresExecutable(adapterName string) bool {
	adapter, ok := adapters[strings.ToLower(strings.TrimSpace(adapterName))]
	if !ok {
		return false
	}
	_, ok = adapter.(cliAdapter)
	return ok
}

func resolveAdapter(name string) (Adapter, error) {
	adapter, ok := adapters[name]
	if !ok {
		return nil, fmt.Errorf("worker adapter must be one of: %s", strings.Join(adapterNames(), ", "))
	}
	return adapter, nil
}

func adapterNames() []string {
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
