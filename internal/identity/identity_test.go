package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestGenerateEncryptedRoundTripsWithCorrectPassphrase(t *testing.T) {
	c, err := GenerateEncrypted("proj", "Alex:elevated", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Encrypted || c.Salt == "" || c.Nonce == "" {
		t.Fatalf("expected an encrypted credential with salt/nonce set, got %+v", c)
	}
	decrypted, err := c.Decrypted("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.Encrypted {
		t.Fatal("expected Decrypted to clear the Encrypted flag")
	}
	raw, err := base64.StdEncoding.DecodeString(decrypted.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		t.Fatalf("decrypted private key has wrong length: got %d, want %d", len(raw), ed25519.PrivateKeySize)
	}
	// The decrypted key must actually correspond to the credential's public
	// key -- not just be the right length.
	pub, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(raw), []byte("probe"))
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte("probe"), sig) {
		t.Fatal("decrypted private key does not match the credential's public key")
	}
}

func TestDecryptedRejectsWrongPassphrase(t *testing.T) {
	c, err := GenerateEncrypted("proj", "Alex:elevated", "the real passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Decrypted("a guess"); err == nil {
		t.Fatal("expected an incorrect passphrase to be rejected")
	}
}

func TestDecryptedRejectsMalformedNonceWithoutPanic(t *testing.T) {
	c, err := GenerateEncrypted("proj", "actor:elevated", "synthetic-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 11, 13} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			broken := c
			broken.Nonce = base64.StdEncoding.EncodeToString(make([]byte, size))
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("corrupt nonce length %d panicked instead of returning an error: %v", size, recovered)
				}
			}()
			if _, err := broken.Decrypted("synthetic-passphrase"); err == nil {
				t.Fatal("corrupt nonce must be rejected")
			}
		})
	}
}

func TestDecryptedDetectsCiphertextTampering(t *testing.T) {
	c, err := GenerateEncrypted("proj", "Alex:elevated", "the real passphrase")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xFF // flip a bit in the ciphertext
	c.PrivateKey = base64.StdEncoding.EncodeToString(raw)
	if _, err = c.Decrypted("the real passphrase"); err == nil {
		t.Fatal("expected AES-GCM authentication to catch tampered ciphertext even with the correct passphrase")
	}
}

func TestDecryptedIsNoOpOnUnencryptedCredential(t *testing.T) {
	c, err := Generate("proj", "Alex")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := c.Decrypted("anything, or nothing at all")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.PrivateKey != c.PrivateKey {
		t.Fatal("expected an unencrypted credential to pass through Decrypted unchanged")
	}
}

func TestElevatedActorIsDistinctFromPrimary(t *testing.T) {
	if got := ElevatedActor("Alex"); got == "Alex" || got != "Alex:elevated" {
		t.Fatalf("ElevatedActor(%q) = %q, want a distinct, stable account name", "Alex", got)
	}
}

func TestResolveActorPrecedenceAndProjectIsolation(t *testing.T) {
	config := UserConfig{
		ActiveProfile: "other:WRONG",
		ActiveProfileBySession: map[string]SessionProfile{
			"session-A": {Profile: "project:SESSIONACTOR", SetAt: time.Now()},
		},
		Profiles: map[string]Profile{
			"project:claude-axiom": {Name: "project:claude-axiom", ProjectID: "project", Actor: "claude-axiom", HostLabel: "claude"},
			"other:WRONG":          {Name: "other:WRONG", ProjectID: "other", Actor: "WRONG"},
			"project:SESSIONACTOR": {Name: "project:SESSIONACTOR", ProjectID: "project", Actor: "SESSIONACTOR"},
		},
	}
	tests := []struct {
		name    string
		request ActorResolutionRequest
		actor   string
		source  string
	}{
		{
			name: "explicit actor overrides every indirect source",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ExplicitActor: "claude-damon",
				ExplicitProfile: "project:claude-axiom", EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "claude-damon", source: ActorSourceFlag,
		},
		{
			name: "explicit profile overrides environment",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "project:claude-axiom",
				EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "claude-axiom", source: ActorSourceProfileFlag,
		},
		{
			name: "environment overrides host binding",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", EnvironmentActor: "ENV",
				HostLabel: "claude", UserConfig: config,
			},
			actor: "ENV", source: ActorSourceEnvironment,
		},
		{
			name: "host binding resolves within project",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", HostLabel: "claude", UserConfig: config,
			},
			actor: "claude-axiom", source: ActorSourceHostBinding,
		},
		{
			name: "cross-project active profile never leaks",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", UserConfig: config,
			},
			actor: "owner", source: ActorSourceProjectOwner,
		},
		{
			// This is the regression test for the real, confirmed-live
			// defect this RFC (0016) closes: a session with its own
			// recognized provider session ID must resolve to ITS OWN
			// active profile, not the shared legacy ActiveProfile every
			// other process on the machine would otherwise inherit.
			name: "a recognized session resolves its own active profile, not the legacy machine-wide one",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ProviderSessionID: "session-A", UserConfig: config,
			},
			actor: "SESSIONACTOR", source: ActorSourceSessionProfile,
		},
		{
			// The other half of the same regression: a *different*,
			// recognized-but-unset session must fall through to the safe
			// project-owner default -- never inherit the legacy
			// machine-wide ActiveProfile ("WRONG") either. That fallthrough
			// is exactly the cross-session leak this type exists to close.
			name: "a different session with no active profile of its own never inherits the legacy machine-wide one",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ProviderSessionID: "session-B", UserConfig: config,
			},
			actor: "owner", source: ActorSourceProjectOwner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolution, err := ResolveActor(test.request)
			if err != nil {
				t.Fatal(err)
			}
			if resolution.Actor != test.actor || resolution.Source != test.source {
				t.Fatalf("unexpected resolution: %+v", resolution)
			}
		})
	}
}

// TestResolveActorPreferOwnerOnAmbiguousLegacy is the regression test for
// RFC 0019: the TUI's own opt-out of RFC 0017's ambiguous-legacy-actor
// tier, scoped narrowly to exactly that one tier.
func TestResolveActorPreferOwnerOnAmbiguousLegacy(t *testing.T) {
	ambiguous := UserConfig{
		ActiveProfile: "project:AGENT_A",
		Profiles: map[string]Profile{
			"project:AGENT_A": {Name: "project:AGENT_A", ProjectID: "project", Actor: "AGENT_A"},
			"project:AGENT_B": {Name: "project:AGENT_B", ProjectID: "project", Actor: "AGENT_B"},
		},
	}
	res, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", UserConfig: ambiguous,
		PreferOwnerOnAmbiguousLegacy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actor != "owner" || res.Source != ActorSourceProjectOwner {
		t.Fatalf("expected the TUI to resolve straight to the project owner when the legacy tier is ambiguous, got %+v", res)
	}

	// Same request without the flag (CLI/MCP/worker): uses the legacy
	// field exactly as RFC 0017 already did -- completely unaffected.
	res, err = ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", UserConfig: ambiguous,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actor != "AGENT_A" || res.Source != ActorSourceActiveProfile {
		t.Fatalf("expected the unaffected legacy resolution for a non-TUI caller, got %+v", res)
	}

	unambiguous := UserConfig{
		ActiveProfile: "project:SOLO",
		Profiles: map[string]Profile{
			"project:SOLO": {Name: "project:SOLO", ProjectID: "project", Actor: "SOLO"},
		},
	}
	// Only one locally-registered identity: not ambiguous, so
	// PreferOwnerOnAmbiguousLegacy has nothing to redirect -- resolves
	// normally even with the flag set.
	res, err = ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", UserConfig: unambiguous,
		PreferOwnerOnAmbiguousLegacy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actor != "SOLO" || res.Source != ActorSourceActiveProfile {
		t.Fatalf("expected a genuinely unambiguous legacy resolution to be unaffected by PreferOwnerOnAmbiguousLegacy, got %+v", res)
	}

	// An explicit actor still wins outright, regardless of the flag.
	res, err = ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", UserConfig: ambiguous,
		ExplicitActor: "AGENT_B", PreferOwnerOnAmbiguousLegacy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actor != "AGENT_B" || res.Source != ActorSourceFlag {
		t.Fatalf("expected an explicit --actor to still win over PreferOwnerOnAmbiguousLegacy, got %+v", res)
	}
}

func TestResolveActorRejectsAmbiguousHostBinding(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", HostLabel: "claude",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"project:claude-axiom": {Name: "project:claude-axiom", ProjectID: "project", Actor: "claude-axiom", HostLabel: "claude"},
			"project:PRISM":        {Name: "project:PRISM", ProjectID: "project", Actor: "PRISM", HostLabel: "claude"},
		}},
	})
	if err == nil {
		t.Fatal("expected ambiguous host binding to fail")
	}
}

func TestResolveActorRejectsProfileFromAnotherProject(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "other:claude-axiom",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"other:claude-axiom": {Name: "other:claude-axiom", ProjectID: "other", Actor: "claude-axiom"},
		}},
	})
	if err == nil {
		t.Fatal("expected cross-project profile selection to fail")
	}
}

// TestActiveProfileForAndSetActiveProfileFor is the direct unit test for
// RFC 0016's session-isolation invariants: two different sessions setting
// different profiles never see each other's value, a genuine plain
// terminal (empty sessionID) uses the legacy field exactly as before, and
// a real-but-unset session never falls through to that legacy field.
func TestProfileCountForProject(t *testing.T) {
	c := UserConfig{Profiles: map[string]Profile{
		"proj-a:owner": {Name: "proj-a:owner", ProjectID: "proj-a", Actor: "owner"},
		"proj-a:THOR":  {Name: "proj-a:THOR", ProjectID: "proj-a", Actor: "THOR"},
		"proj-a:ZEUS":  {Name: "proj-a:ZEUS", ProjectID: "proj-a", Actor: "ZEUS"},
		"proj-b:owner": {Name: "proj-b:owner", ProjectID: "proj-b", Actor: "owner"},
	}}
	if got := c.ProfileCountForProject("proj-a"); got != 3 {
		t.Fatalf("ProfileCountForProject(proj-a) = %d, want 3", got)
	}
	if got := c.ProfileCountForProject("proj-b"); got != 1 {
		t.Fatalf("ProfileCountForProject(proj-b) = %d, want 1", got)
	}
	if got := c.ProfileCountForProject("proj-c"); got != 0 {
		t.Fatalf("ProfileCountForProject(proj-c) = %d, want 0", got)
	}
}

func TestActiveProfileForAndSetActiveProfileFor(t *testing.T) {
	var c UserConfig

	// Plain terminal (no session ID): legacy field, exactly the pre-RFC
	// behavior, unchanged.
	c.SetActiveProfileFor("", "legacy-profile")
	if got := c.ActiveProfileFor(""); got != "legacy-profile" {
		t.Fatalf("ActiveProfileFor(\"\") = %q, want legacy-profile", got)
	}
	if c.ActiveProfile != "legacy-profile" {
		t.Fatalf("legacy ActiveProfile field = %q, want legacy-profile", c.ActiveProfile)
	}

	// Two different sessions: fully isolated from each other and from the
	// legacy field.
	c.SetActiveProfileFor("session-A", "profile-A")
	c.SetActiveProfileFor("session-B", "profile-B")
	if got := c.ActiveProfileFor("session-A"); got != "profile-A" {
		t.Fatalf("session-A resolved %q, want profile-A", got)
	}
	if got := c.ActiveProfileFor("session-B"); got != "profile-B" {
		t.Fatalf("session-B resolved %q, want profile-B", got)
	}
	if got := c.ActiveProfileFor(""); got != "legacy-profile" {
		t.Fatalf("legacy field changed to %q after setting session profiles, want it untouched (legacy-profile)", got)
	}

	// A real, recognized session with nothing set for it yet must resolve
	// to "" -- never fall through to the legacy field. This is the exact
	// leak this type exists to close.
	if got := c.ActiveProfileFor("session-C"); got != "" {
		t.Fatalf("unset session-C resolved %q, want \"\" (must not inherit the legacy field)", got)
	}
}

// TestSetActiveProfileForPrunesStaleSessions confirms the TTL-bounded
// pruning: an old session entry doesn't accumulate forever, but a fresh
// one (or the session currently being written) is never pruned regardless
// of age.
func TestSetActiveProfileForPrunesStaleSessions(t *testing.T) {
	c := UserConfig{ActiveProfileBySession: map[string]SessionProfile{
		"stale":   {Profile: "old", SetAt: time.Now().Add(-2 * sessionProfileTTL)},
		"current": {Profile: "current-profile", SetAt: time.Now()},
	}}
	c.SetActiveProfileFor("new-session", "new-profile")
	if _, ok := c.ActiveProfileBySession["stale"]; ok {
		t.Fatal("expected the stale session entry to be pruned")
	}
	if got := c.ActiveProfileFor("current"); got != "current-profile" {
		t.Fatalf("expected the fresh session entry to survive pruning, got %q", got)
	}
	if got := c.ActiveProfileFor("new-session"); got != "new-profile" {
		t.Fatalf("expected the just-set session entry to be present, got %q", got)
	}
}

func TestDetectProviderSessionIDReadsClaudeAndCodexEnv(t *testing.T) {
	// t.Setenv to "" (not left ambient) deliberately: this test suite
	// itself typically runs inside a real Claude Code session, which sets
	// CLAUDE_CODE_SESSION_ID in the actual process environment -- clearing
	// both explicitly, rather than assuming a "clean" environment, is what
	// makes this test reliable regardless of what's running it.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")
	if got := DetectProviderSessionID(); got != "" {
		t.Fatalf("expected no session ID with both provider vars cleared, got %q", got)
	}
	t.Setenv("CODEX_THREAD_ID", "codex-123")
	if got := DetectProviderSessionID(); got != "codex-123" {
		t.Fatalf("DetectProviderSessionID() = %q, want codex-123", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "claude-456")
	if got := DetectProviderSessionID(); got != "claude-456" {
		t.Fatalf("expected Claude to take priority over Codex, got %q", got)
	}
}

func TestHostIDIsRandomStableAndPrivate(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", configDir)
	first, err := LoadOrCreateHostID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateHostID()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("host ID was not stable: first=%q second=%q", first, second)
	}
	info, err := os.Stat(filepath.Join(configDir, hostIDFileName))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("host ID permissions=%#o, want 0600", info.Mode().Perm())
	}
}

func TestLoadUserConfigIgnoresLegacyTheme(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", configDir)
	legacy := []byte(`{"theme":"dark","update_channel":"stable","profiles":{}}`)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.UpdateChannel != "stable" || config.Profiles == nil {
		t.Fatalf("legacy theme must not prevent reading other settings: %+v", config)
	}
}
