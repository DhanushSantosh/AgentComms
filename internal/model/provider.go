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

// The set is fixed at build time, deliberately.
//
// It was briefly extensible: registering a declarative adapter called a
// RegisterProvider that mutated a package-level map. That does not work and
// cannot be made to work this way. Provider validity is enforced in
// ValidateTransition, which runs inside the authority process -- the local
// daemon in personal mode, a remote service in team mode -- while adapter
// files are loaded by the CLI process. Verified end to end: with an adapter
// declared, the CLI accepted `--provider housecat` and the daemon rejected
// the resulting command, reporting only the three built-ins. Loading
// adapters in the daemon too would paper over it in personal mode, leave
// team mode broken (a remote authority has no access to the project's
// files), and introduce a write to this map concurrent with the reads
// validation performs.
//
// The deeper objection is that it is the wrong shape for this project: a
// local JSON file would silently widen which identities a *signed*
// authority accepts. If project-scoped providers are wanted, they belong in
// signed project state, so every process reaches the same answer. See
// docs/backlog.md, "Project-scoped custom providers".

// KnownProviders lists every accepted provider, sorted, for error messages
// and help text.
func KnownProviders() []string {
	names := make([]string, 0, len(builtInProviders))
	for name := range builtInProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsKnownProvider reports whether name is an accepted provider.
func IsKnownProvider(name string) bool {
	return builtInProviders[strings.ToLower(strings.TrimSpace(name))]
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
	// Only suggest a replacement that would itself pass. Suggesting
	// "claude-foo_bar" for "foo_bar", or "reviewer" for "Reviewer", sends
	// the caller to a second identical failure and makes the message worse
	// than no suggestion at all.
	if suggestion, ok := suggestedActorID(actorID); ok {
		return fmt.Errorf("agent ID %q must name its provider: try %q. Known providers: %s",
			actorID, suggestion, providers)
	}
	return fmt.Errorf(
		"agent ID %q must name its provider: use <provider> or <provider>-<suffix>, "+
			"where <suffix> is lower-case letters, digits and hyphens. Known providers: %s",
		actorID, providers,
	)
}

// suggestedActorID derives a valid ID from what the caller typed, or
// reports that nothing salvageable could be formed. It never returns a
// value that ValidateAgentActorID would reject.
func suggestedActorID(actorID string) (string, bool) {
	candidate := strings.ToLower(strings.TrimSpace(actorID))
	if candidate == "" {
		return "", false
	}
	// Case or surrounding space was the only problem: suggest the
	// normalized form rather than prefixing an already-valid ID into
	// "claude-claude-main".
	if _, ok := ProviderOf(candidate); ok {
		return candidate, true
	}
	// Already provider-prefixed but otherwise malformed (bad suffix
	// characters): there is no single obvious repair, so say nothing.
	if _, ok := ProviderOf(candidate); !ok {
		for _, provider := range KnownProviders() {
			if candidate == provider || strings.HasPrefix(candidate, provider+"-") {
				return "", false
			}
		}
	}
	if !actorSuffix.MatchString(candidate) {
		return "", false
	}
	suggestion := defaultSuggestionProvider + "-" + candidate
	if _, ok := ProviderOf(suggestion); !ok {
		return "", false
	}
	return suggestion, true
}

// defaultSuggestionProvider is the provider used when illustrating a fixed
// ID. It only ever appears inside an error message.
const defaultSuggestionProvider = "claude"

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
