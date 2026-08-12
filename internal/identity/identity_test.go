package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGenerateEncryptedRoundTripsWithCorrectPassphrase(t *testing.T) {
	c, err := GenerateEncrypted("proj", "Dhanush:elevated", "correct horse battery staple")
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
	c, err := GenerateEncrypted("proj", "Dhanush:elevated", "the real passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Decrypted("a guess"); err == nil {
		t.Fatal("expected an incorrect passphrase to be rejected")
	}
}

func TestDecryptedDetectsCiphertextTampering(t *testing.T) {
	c, err := GenerateEncrypted("proj", "Dhanush:elevated", "the real passphrase")
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
	c, err := Generate("proj", "Dhanush")
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
	if got := ElevatedActor("Dhanush"); got == "Dhanush" || got != "Dhanush:elevated" {
		t.Fatalf("ElevatedActor(%q) = %q, want a distinct, stable account name", "Dhanush", got)
	}
}

func TestFindProfileByProjectAndHost(t *testing.T) {
	profiles := map[string]Profile{
		"p1:AXIOM": {Name: "p1:AXIOM", ProjectID: "p1", Actor: "AXIOM", HostLabel: "claude"},
		"p1:DAMON": {Name: "p1:DAMON", ProjectID: "p1", Actor: "DAMON", HostLabel: "codex"},
		"p2:HENRY": {Name: "p2:HENRY", ProjectID: "p2", Actor: "HENRY", HostLabel: "claude"},
	}
	actor, ok := FindProfileByProjectAndHost(profiles, "p1", "claude")
	if !ok || actor != "AXIOM" {
		t.Fatalf("expected AXIOM, got %q ok=%v", actor, ok)
	}
	actor, ok = FindProfileByProjectAndHost(profiles, "p1", "codex")
	if !ok || actor != "DAMON" {
		t.Fatalf("expected DAMON, got %q ok=%v", actor, ok)
	}
	if _, ok = FindProfileByProjectAndHost(profiles, "p1", "opencode"); ok {
		t.Fatal("expected no match for an unregistered host in a known project")
	}
	if _, ok = FindProfileByProjectAndHost(profiles, "p3", "claude"); ok {
		t.Fatal("expected no match for an unknown project")
	}
}

// TestFindProfileByProjectAndHostAmbiguous guards the deliberate design
// choice not to guess: if a host somehow registered two agents in the same
// project (two profiles sharing project+host), resolution must decline
// rather than picking one arbitrarily, so callers fall back to existing
// resolution behavior instead of silently binding to the wrong identity.
func TestFindProfileByProjectAndHostAmbiguous(t *testing.T) {
	profiles := map[string]Profile{
		"p1:AXIOM": {Name: "p1:AXIOM", ProjectID: "p1", Actor: "AXIOM", HostLabel: "claude"},
		"p1:PRISM": {Name: "p1:PRISM", ProjectID: "p1", Actor: "PRISM", HostLabel: "claude"},
	}
	if _, ok := FindProfileByProjectAndHost(profiles, "p1", "claude"); ok {
		t.Fatal("expected ambiguous multi-match to return ok=false")
	}
}

func TestResolveActorPrecedenceAndProjectIsolation(t *testing.T) {
	config := UserConfig{
		ActiveProfile: "other:WRONG",
		ActiveProfileBySession: map[string]SessionProfile{
			"session-A": {Profile: "project:SESSIONACTOR", SetAt: time.Now()},
		},
		Profiles: map[string]Profile{
			"project:AXIOM":        {Name: "project:AXIOM", ProjectID: "project", Actor: "AXIOM", HostLabel: "claude"},
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
				ProjectID: "project", ProjectOwner: "owner", ExplicitActor: "DAMON",
				ExplicitProfile: "project:AXIOM", EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "DAMON", source: ActorSourceFlag,
		},
		{
			name: "explicit profile overrides environment",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "project:AXIOM",
				EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "AXIOM", source: ActorSourceProfileFlag,
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
			actor: "AXIOM", source: ActorSourceHostBinding,
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

func TestResolveActorRejectsAmbiguousHostBinding(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", HostLabel: "claude",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"project:AXIOM": {Name: "project:AXIOM", ProjectID: "project", Actor: "AXIOM", HostLabel: "claude"},
			"project:PRISM": {Name: "project:PRISM", ProjectID: "project", Actor: "PRISM", HostLabel: "claude"},
		}},
	})
	if err == nil {
		t.Fatal("expected ambiguous host binding to fail")
	}
}

func TestResolveActorRejectsProfileFromAnotherProject(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "other:AXIOM",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"other:AXIOM": {Name: "other:AXIOM", ProjectID: "other", Actor: "AXIOM"},
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
