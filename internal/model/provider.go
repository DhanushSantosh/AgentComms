package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Providers are the AI runtimes an AGENT principal can be backed by. An
// agent's actor ID begins with one, so that "which runtime produced this
// event" is answerable from the identity alone -- the actor ID is what
// appears in every event, table and log line, and a separate metadata field
// would not be visible where the question actually gets asked. See RFC 0039.
//
// Deliberately not derived from internal/worker.builtInAdapters: that list
// includes transport variants (claude-acp, codex-live) which are not
// separate providers, and a principal named claude-acp-main would be wrong.
var builtInProviders = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
}

// extraProviders holds providers contributed by declarative adapters, so a
// project can add its own runtime without a code change -- the same escape
// hatch the adapter system already offers. Registration happens at startup,
// before any command is validated.
var extraProviders = map[string]bool{}

// RegisterProvider adds a provider name to the accepted set. Names are
// lower-cased; blank names are ignored rather than creating an entry that
// could never match.
func RegisterProvider(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || builtInProviders[name] {
		return
	}
	extraProviders[name] = true
}

// KnownProviders lists every accepted provider, sorted, for error messages
// and help text.
func KnownProviders() []string {
	names := make([]string, 0, len(builtInProviders)+len(extraProviders))
	for name := range builtInProviders {
		names = append(names, name)
	}
	for name := range extraProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsKnownProvider reports whether name is an accepted provider.
func IsKnownProvider(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return builtInProviders[name] || extraProviders[name]
}

// actorSuffix matches the optional part after "<provider>-". Lower case so
// two principals cannot differ only by case, which reads as the same
// identity to a human scanning a table.
var actorSuffix = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ProviderOf returns the provider an agent actor ID names, and whether the
// ID is well formed. Accepts "<provider>" and "<provider>-<suffix>".
func ProviderOf(actorID string) (string, bool) {
	if actorID == "" || actorID != strings.ToLower(actorID) {
		return "", false
	}
	if IsKnownProvider(actorID) {
		return actorID, true
	}
	// Longest match wins so a provider containing a hyphen still resolves
	// correctly against one that is a prefix of it.
	best := ""
	for _, provider := range KnownProviders() {
		if strings.HasPrefix(actorID, provider+"-") && len(provider) > len(best) {
			best = provider
		}
	}
	if best == "" {
		return "", false
	}
	if !actorSuffix.MatchString(strings.TrimPrefix(actorID, best+"-")) {
		return "", false
	}
	return best, true
}

// ValidateAgentActorID enforces RFC 0039's grammar for AGENT principals.
// HUMAN principals are not checked anywhere -- a person is not a provider.
//
// The error names the expected form rather than only stating a rule,
// because the common mistake is a bare role name ("claude-reviewer") and the fix
// is mechanical ("claude-reviewer").
func ValidateAgentActorID(actorID string) error {
	if _, ok := ProviderOf(actorID); ok {
		return nil
	}
	providers := strings.Join(KnownProviders(), ", ")
	if lowered := strings.ToLower(actorID); lowered != actorID {
		return fmt.Errorf("agent ID %q must be lower case; try %q", actorID, lowered)
	}
	return fmt.Errorf(
		"agent ID %q must name its provider: use <provider> or <provider>-<suffix> (e.g. claude-%s). Known providers: %s",
		actorID, actorID, providers,
	)
}

// DefaultAgentActorID returns the ID to use when none was supplied:
// "<provider>", or "<provider>-2", "-3", ... when earlier ones are taken.
func DefaultAgentActorID(provider string, taken func(string) bool) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if !taken(provider) {
		return provider
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", provider, n)
		if !taken(candidate) {
			return candidate
		}
	}
}
