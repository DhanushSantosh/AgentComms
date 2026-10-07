package model

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
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
//
// The built-ins are the floor every project accepts. A project adds more by
// signed provider.register events (RFC 0050), so the accepted set is derived
// from history and every process holding that history -- CLI, daemon, a
// remote team authority -- reaches the same answer. RFC 0039's first attempt
// let a local declarative adapter file add names; adapter files load in the
// CLI while validation runs in the authority, so the two disagreed. Adapter
// files still never affect identity.
var builtInProviders = []string{"claude", "codex", "opencode"}

// Provider statuses.
const (
	ProviderStatusActive  = "ACTIVE"
	ProviderStatusRetired = "RETIRED"
)

// Bounds for a registered provider's free-text fields (RFC 0050).
const (
	MaxProviderDisplayName  = 64
	MaxProviderDescription  = 512
	MaxProviderRetireReason = 512
)

// providerName is RFC 0050's grammar for a registered provider. No hyphens:
// an agent ID splits at the hyphen after its provider, so a provider named
// "claude-code" would turn the existing agent claude-code-reviewer (provider
// claude) into a different identity.
var providerName = regexp.MustCompile(`^[a-z][a-z0-9]{1,23}$`)

// reservedProviderNames read as roles or tooling rather than runtimes.
var reservedProviderNames = map[string]bool{
	"agent": true, "human": true, "system": true, "owner": true,
	"orchestrator": true, "observer": true, "worker": true, "agc": true,
	"agentcomms": true, "unknown": true, "none": true, "all": true,
}

// ProviderSet is the set of provider names agent IDs are checked against.
type ProviderSet struct {
	names map[string]bool
}

// NewProviderSet returns a set holding the built-ins plus extra.
func NewProviderSet(extra ...string) ProviderSet {
	set := ProviderSet{names: make(map[string]bool, len(builtInProviders)+len(extra))}
	for _, name := range builtInProviders {
		set.names[name] = true
	}
	for _, name := range extra {
		set.names[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return set
}

// BuiltInProviders is the set every project accepts before any
// provider.register event.
func BuiltInProviders() ProviderSet { return NewProviderSet() }

// RegistrableProviders is the set a new agent.register may use: the
// built-ins plus every ACTIVE registered provider.
func RegistrableProviders(st State) ProviderSet {
	extra := make([]string, 0, len(st.Providers))
	for name, provider := range st.Providers {
		if provider.Status == ProviderStatusActive {
			extra = append(extra, name)
		}
	}
	return NewProviderSet(extra...)
}

// RecognizedProviders also includes RETIRED providers. Use it to read an
// existing agent ID: retirement stops new registrations, it does not make
// an existing agent's provider unknown.
func RecognizedProviders(st State) ProviderSet {
	extra := make([]string, 0, len(st.Providers))
	for name := range st.Providers {
		extra = append(extra, name)
	}
	return NewProviderSet(extra...)
}

// Names lists the set, sorted, for error messages and help text.
func (p ProviderSet) Names() []string {
	names := make([]string, 0, len(p.names))
	for name := range p.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Has reports whether name is in the set.
func (p ProviderSet) Has(name string) bool {
	return p.names[strings.ToLower(strings.TrimSpace(name))]
}

// IsBuiltInProvider reports whether name is one of the build-time providers.
func IsBuiltInProvider(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, builtIn := range builtInProviders {
		if name == builtIn {
			return true
		}
	}
	return false
}

// KnownProviders lists the built-in providers, sorted. Prefer a
// project-derived ProviderSet wherever state is available.
func KnownProviders() []string { return BuiltInProviders().Names() }

// IsKnownProvider reports whether name is a built-in provider.
func IsKnownProvider(name string) bool { return BuiltInProviders().Has(name) }

// ProviderOf reads an agent actor ID against the built-in providers.
func ProviderOf(actorID string) (string, bool) { return BuiltInProviders().ProviderOf(actorID) }

// ValidateAgentActorID checks an agent actor ID against the built-ins.
func ValidateAgentActorID(actorID string) error {
	return BuiltInProviders().ValidateAgentActorID(actorID)
}

// actorSuffix matches the optional part after "<provider>-". Lower case so
// two principals cannot differ only by case, which reads as the same
// identity to a human scanning a table.
var actorSuffix = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ProviderOf returns the provider an agent actor ID names, and whether the
// ID is well formed. Accepts "<provider>" and "<provider>-<suffix>".
func (p ProviderSet) ProviderOf(actorID string) (string, bool) {
	if actorID == "" || actorID != strings.ToLower(actorID) {
		return "", false
	}
	if p.names[actorID] {
		return actorID, true
	}
	// Longest match wins. Registered names cannot contain hyphens, so this
	// is only a defensive tie-break.
	best := ""
	for provider := range p.names {
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
// because the common mistake is a bare role name ("reviewer") and the fix
// is mechanical ("claude-reviewer").
func (p ProviderSet) ValidateAgentActorID(actorID string) error {
	if _, ok := p.ProviderOf(actorID); ok {
		return nil
	}
	providers := strings.Join(p.Names(), ", ")
	// Only suggest a replacement that would itself pass. Suggesting
	// "claude-foo_bar" for "foo_bar", or "reviewer" for "Reviewer", sends
	// the caller to a second identical failure and makes the message worse
	// than no suggestion at all.
	if suggestion, ok := p.suggestedActorID(actorID); ok {
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
func (p ProviderSet) suggestedActorID(actorID string) (string, bool) {
	candidate := strings.ToLower(strings.TrimSpace(actorID))
	if candidate == "" {
		return "", false
	}
	// Case or surrounding space was the only problem: suggest the
	// normalized form rather than prefixing an already-valid ID into
	// "claude-claude-main".
	if _, ok := p.ProviderOf(candidate); ok {
		return candidate, true
	}
	// Already provider-prefixed but otherwise malformed (bad suffix
	// characters): there is no single obvious repair, so say nothing.
	for provider := range p.names {
		if candidate == provider || strings.HasPrefix(candidate, provider+"-") {
			return "", false
		}
	}
	if !actorSuffix.MatchString(candidate) {
		return "", false
	}
	suggestion := defaultSuggestionProvider + "-" + candidate
	if _, ok := p.ProviderOf(suggestion); !ok {
		return "", false
	}
	return suggestion, true
}

// defaultSuggestionProvider is the provider used when illustrating a fixed
// ID. It only ever appears inside an error message.
const defaultSuggestionProvider = "claude"

// ValidateProviderName checks a name for provider.register: RFC 0050's
// grammar, not a built-in, not reserved. Collisions with existing
// principals need state and are checked by the transition validator.
func ValidateProviderName(name string) error {
	if IsBuiltInProvider(name) {
		return fmt.Errorf("%q is a built-in provider and is always accepted", name)
	}
	if !providerName.MatchString(name) {
		return fmt.Errorf("provider name %q must be 2-24 lower-case letters and digits, starting with a letter (no hyphens)", name)
	}
	if reservedProviderNames[name] {
		return fmt.Errorf("provider name %q is reserved", name)
	}
	return nil
}

// ValidateProviderText bounds a registered provider's free-text field.
func ValidateProviderText(field, value string, limit int) error {
	if len([]rune(value)) > limit {
		return fmt.Errorf("provider %s must be at most %d characters", field, limit)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("provider %s must not contain control characters", field)
		}
	}
	return nil
}

// ErrProviderNotRegistered marks an agent registration whose provider the
// project does not accept, so callers can offer to register it (RFC 0050).
var ErrProviderNotRegistered = errors.New("provider is not registered for this project")

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

// ProviderListing is one provider as listed to a reader: the built-ins,
// which are not stored in project state, alongside registered providers.
type ProviderListing struct {
	Provider
	BuiltIn      bool     `json:"built_in"`
	ActiveAgents []string `json:"active_agents"`
}

// ProviderListings lists every provider the project recognizes, keyed by
// name, with an order that puts the built-ins first and then registered
// providers by name.
func ProviderListings(st State) (map[string]ProviderListing, []string) {
	listings := map[string]ProviderListing{}
	recognized := RecognizedProviders(st)
	for _, name := range recognized.Names() {
		listing := ProviderListing{Provider: st.Providers[name], BuiltIn: IsBuiltInProvider(name), ActiveAgents: []string{}}
		listing.Name = name
		if listing.BuiltIn {
			listing.Status = ProviderStatusActive
		}
		listings[name] = listing
	}
	for id, agent := range st.Agents {
		if agent.PrincipalType != PrincipalAgent || agent.Status != "ACTIVE" {
			continue
		}
		if name, ok := recognized.ProviderOf(id); ok {
			listing := listings[name]
			listing.ActiveAgents = append(listing.ActiveAgents, id)
			listings[name] = listing
		}
	}
	order := make([]string, 0, len(listings))
	for name, listing := range listings {
		sort.Strings(listing.ActiveAgents)
		listings[name] = listing
		order = append(order, name)
	}
	sort.Slice(order, func(i, j int) bool {
		left, right := listings[order[i]], listings[order[j]]
		if left.BuiltIn != right.BuiltIn {
			return left.BuiltIn
		}
		return order[i] < order[j]
	})
	return listings, order
}
