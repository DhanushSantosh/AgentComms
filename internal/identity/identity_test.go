package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
		Profiles: map[string]Profile{
			"project:AXIOM": {Name: "project:AXIOM", ProjectID: "project", Actor: "AXIOM", HostLabel: "claude"},
			"other:WRONG":   {Name: "other:WRONG", ProjectID: "other", Actor: "WRONG"},
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
