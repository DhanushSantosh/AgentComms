package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// permissionSignatureByAdapter maps an adapter name to whatever mechanism
// it actually uses to make PermissionMode change subprocess behavior --
// Arguments() (CLI flags) for claude and agy, the environment variable
// opencodePermissionEnv builds for opencode (its Arguments() never
// references PermissionMode at all; the difference lives entirely in the
// child process's environment, set up separately in Execute()). Adapters
// not listed here either don't participate in PermissionMode-driven
// behavior (codex uses Sandbox as its own, separate permission concept and
// never reads PermissionMode) or aren't covered by this contract test's
// scope (ACP/-live adapters, which don't build a plain exec argv this way
// at all).
var permissionSignatureByAdapter = map[string]func(Config) string{
	"claude":   func(c Config) string { return strings.Join(claudeAdapter{}.Arguments(c), "\x00") },
	"opencode": func(c Config) string { return opencodePermissionEnv(c.PermissionMode) },
}

// TestAdapterDefaultPermissionModeIsNotANoOp is the regression test for a
// real, shipped bug: agyAdapter's Validate defaulted an unset PermissionMode
// to "acceptEdits" -- the single most common case, since it's what every
// caller gets who doesn't explicitly set one -- but Arguments() had no case
// for that value at all, so the defaulted config produced byte-identical
// output to a config that never had PermissionMode touched in the first
// place. The default silently behaved as if it didn't exist. Found only by
// a human manually comparing Validate's switch statement against
// Arguments()'s by eye.
//
// This is deliberately narrower than "every accepted PermissionMode must be
// pairwise distinct from every other" -- that stronger version was tried
// first and produced false positives against real, intentional design: agy
// treats "acceptEdits" and "accept-edits" as two spellings of the same
// level on purpose, and opencode's own documented design collapses several
// modes into the same coarse allow/ask split. Comparing only the defaulted
// config against a config with PermissionMode left completely untouched
// catches exactly the "default is silently inert" failure without
// penalizing legitimate aliasing.
func TestAdapterDefaultPermissionModeIsNotANoOp(t *testing.T) {
	for name, signature := range permissionSignatureByAdapter {
		name, signature := name, signature
		t.Run(name, func(t *testing.T) {
			adapter, ok := adapters[name]
			if !ok {
				t.Fatalf("permissionSignatureByAdapter references %q, which isn't in the adapters map", name)
			}
			execPath := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			base := Config{
				Actor: "test-agent", Executable: execPath, SessionID: "sess-1",
				ClaudeBudgetUSD: 1, // only claudeAdapter checks this; harmless for the others
			}

			untouched := signature(base) // PermissionMode never set at all

			defaulted := base
			if err := adapter.Validate(&defaulted); err != nil {
				t.Fatalf("%s.Validate failed on an otherwise-minimal config: %v", name, err)
			}
			if defaulted.PermissionMode == "" {
				t.Skipf("%s.Validate leaves PermissionMode empty by default; nothing to check", name)
			}

			if signature(defaulted) == untouched {
				t.Errorf("%s: the default PermissionMode (%q, what Validate fills in when it's left unset) produces identical invocation behavior (%q) to never setting PermissionMode at all -- the default is silently a no-op, exactly the agy bug this test exists to catch",
					name, defaulted.PermissionMode, untouched)
			}
		})
	}
}

// TestAdapterPermissionModesProduceDistinctBehavior generalizes the same
// class of comparison across every value an adapter's own Validate accepts,
// for adapters that document no intentional aliasing among the candidate
// set below -- claude, whose Arguments() passes config.PermissionMode
// through to --permission-mode verbatim, so every accepted value should be
// its own distinct flag value. (agy and opencode are excluded here, not
// from the map above: both have real, documented aliasing among these
// candidates -- see TestAdapterDefaultPermissionModeIsNotANoOp's comment --
// so a pairwise-distinctness assertion would flag intentional design as a
// bug for them specifically.)
func TestAdapterPermissionModesProduceDistinctBehavior(t *testing.T) {
	candidateModes := []string{"acceptEdits", "auto", "dontAsk", "manual", "plan"}
	pairwiseDistinctAdapters := []string{"claude"}

	for _, name := range pairwiseDistinctAdapters {
		name := name
		t.Run(name, func(t *testing.T) {
			adapter, ok := adapters[name]
			if !ok {
				t.Fatalf("pairwiseDistinctAdapters references %q, which isn't in the adapters map", name)
			}
			signature, ok := permissionSignatureByAdapter[name]
			if !ok {
				t.Fatalf("pairwiseDistinctAdapters references %q, which has no permissionSignatureByAdapter entry", name)
			}
			execPath := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			base := Config{Actor: "test-agent", Executable: execPath, SessionID: "sess-1", ClaudeBudgetUSD: 1}

			seen := map[string]string{}
			accepted := 0
			for _, mode := range candidateModes {
				config := base
				config.PermissionMode = mode
				if err := adapter.Validate(&config); err != nil {
					continue
				}
				accepted++
				key := signature(config)
				if firstMode, dup := seen[key]; dup {
					t.Errorf("%s: modes %q and %q are both accepted by Validate but produce identical invocation behavior (%q) -- if this aliasing is intentional, move %s into permissionSignatureByAdapter's exclusions (see this test's own doc comment) and explain why; if not, this is exactly the class of bug agy shipped with",
						name, firstMode, mode, key, name)
				}
				seen[key] = mode
			}
			if accepted < 2 {
				t.Fatalf("%s: only %d of the candidate PermissionMode values were accepted by Validate; this test needs at least 2 accepted values to compare -- extend candidateModes to cover this adapter's real accepted set", name, accepted)
			}
		})
	}
}
