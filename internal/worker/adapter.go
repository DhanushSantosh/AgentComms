package worker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// Adapter captures everything about a specific AI provider that Worker's
// execution loop must not know: how to validate provider-specific Config
// fields, how to build the provider's CLI invocation, and how to shape the
// task prompt delivered to it. Today every registered adapter execs a
// provider CLI directly; a later phase can register ACP-backed adapters
// (OpenCode, Codex-over-ACP, etc.) implementing the same interface without
// changing Worker at all.
type Adapter interface {
	// Validate normalizes and checks the fields of Config this adapter
	// owns, filling in defaults. It runs after the adapter-agnostic checks
	// in validateConfig.
	Validate(config *Config) error
	// Arguments builds the CLI argv used to invoke the provider.
	Arguments(config Config) []string
	// Prompt builds the task prompt delivered to the provider on stdin.
	Prompt(actor string, invocation model.Invocation) string
}

var adapters = map[string]Adapter{
	"claude": claudeAdapter{},
	"codex":  codexAdapter{},
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
