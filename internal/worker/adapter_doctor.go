package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// adapterSourceFile maps a cliAdapter's registered name to the source file
// where its Arguments()/Validate() logic lives -- the one place a real CLI
// flag assumption should appear as a literal "--foo" string. Used by
// VerifyAdapterFlags to check those assumptions against what the real,
// installed binary's own --help actually documents.
//
// This generalizes, into a repeatable check, the manual technique that
// caught a real bug: sessionbind.go shipped with a wrong environment
// variable name for agy (ANTIGRAVITY_SESSION_ID/AGY_SESSION_ID, neither
// real), found only by running `strings` on the installed agy binary by
// hand and finding the actual variable embedded in a bundled script. --help
// output never lists environment variables, so this tool can't catch that
// exact class of drift -- but the sibling class, a flag string that looks
// plausible in source but the real CLI doesn't actually support (renamed,
// removed, or simply never existed), is exactly what --help does document,
// and is what this checks automatically instead of requiring a human to
// re-run that same manual comparison by hand every time an adapter changes.
// (agy itself is no longer a built-in adapter -- removed 2026-08-08 over an
// unresolved third-party-ToS compliance question, see docs/backlog.md --
// this historical note about how the check was motivated stays accurate
// regardless.)
var adapterSourceFile = map[string]string{
	"claude":   "adapter_claude.go",
	"codex":    "adapter_codex.go",
	"opencode": "adapter_opencode.go",
}

// adapterHelpSubcommand names the subcommand an adapter's own Arguments()
// routes its real, adapter-relevant flags through, when it uses one --
// DocumentedFlags checks this subcommand's own --help in addition to the
// bare executable's, since a subcommand's flags never appear in the
// top-level --help at all. Confirmed live 2026-08-10, filed as issue #18
// and investigated rather than patched over: codex's --color/--ephemeral/
// --ignore-user-config and opencode's --format are all real, currently
// supported flags of `codex exec`/`opencode run` respectively --
// verify-adapter reported them as "missing" purely because it only ever
// checked the bare `<executable> --help`, which never lists
// subcommand-scoped flags at all (confirmed by checking `codex exec --help`
// and `opencode run --help` directly). claude has no entry here: every
// flag its adapter uses is a genuine top-level `claude` flag, which is
// exactly why claude already reported clean without this.
var adapterHelpSubcommand = map[string]string{
	"codex":    "exec",
	"opencode": "run",
}

var flagLiteralPattern = regexp.MustCompile(`--[a-zA-Z][a-zA-Z0-9-]*`)

// AssumedFlags statically scans an adapter's own source file for
// "--flag"-shaped string literals -- the flags its Arguments()/Validate()
// methods (and their doc comments) reference. sourceDir is the directory
// containing adapter source files (internal/worker in production; tests
// and other callers may point it at a throwaway fixture directory).
//
// This scans raw source text, not just string literals inside
// Arguments()/Validate() specifically, so a flag named only in a comment
// shows up as "assumed" too -- deliberately over-inclusive rather than
// under-inclusive, since a false positive here just means a human glances
// at one extra line, while a false negative would silently let the exact
// class of bug this exists to catch back in.
func AssumedFlags(adapterName, sourceDir string) ([]string, error) {
	file, ok := adapterSourceFile[adapterName]
	if !ok {
		return nil, fmt.Errorf("no known source file for adapter %q", adapterName)
	}
	content, err := os.ReadFile(filepath.Join(sourceDir, file))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	return dedupeSorted(flagLiteralPattern.FindAllString(string(content), -1)), nil
}

// DocumentedFlags runs "executable [subcommand...] --help" and extracts
// every "--flag"-shaped token from its output -- a real CLI's own
// advertised flag surface for that exact invocation. subcommand is
// typically empty (bare `<executable> --help`) or one adapterHelpSubcommand
// entry (e.g. `codex exec --help`) -- a subcommand's own flags never
// appear in the bare executable's top-level --help at all, confirmed live
// as the real cause of two false "missing flag" reports (issue #18), not
// assumed.
func DocumentedFlags(ctx context.Context, executable string, subcommand ...string) ([]string, error) {
	args := append(append([]string{}, subcommand...), "--help")
	output, err := exec.CommandContext(ctx, executable, args...).CombinedOutput()
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("run %s %s: %w", executable, strings.Join(args, " "), err)
	}
	// Some CLIs exit non-zero even for --help (cobra's own default
	// convention does not, but not every provider CLI follows it); the
	// output is still useful as long as there is any, so a non-zero exit
	// with real output is not itself treated as a failure here.
	return dedupeSorted(flagLiteralPattern.FindAllString(string(output), -1)), nil
}

func dedupeSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// VerifyAdapterFlags reports every flag an adapter's own source assumes
// exists but the real, installed binary's --help output (bare, plus its
// adapterHelpSubcommand entry's own --help, when it has one) never
// mentions.
//
// Not exhaustive: a flag genuinely supported but undocumented in --help
// would show up as a false positive (missing here doesn't necessarily mean
// broken); an adapter whose comments discuss flags belonging to a different
// version or a related tool would over-report for the same reason
// AssumedFlags is deliberately over-inclusive. This is an audit aid for a
// human to glance at when an adapter changes or a new provider version
// ships, not a hard gate.
func VerifyAdapterFlags(ctx context.Context, adapterName, sourceDir, executable string) (missing []string, err error) {
	assumed, err := AssumedFlags(adapterName, sourceDir)
	if err != nil {
		return nil, err
	}
	documented, err := DocumentedFlags(ctx, executable)
	if err != nil {
		return nil, err
	}
	if subcommand, ok := adapterHelpSubcommand[adapterName]; ok {
		// A real subcommand invocation failing here (binary too old to have
		// it, transient error) shouldn't fail the whole check -- fall back to
		// bare --help alone rather than erroring the audit out entirely.
		subDocumented, subErr := DocumentedFlags(ctx, executable, subcommand)
		if subErr == nil {
			documented = append(documented, subDocumented...)
		}
	}
	present := make(map[string]bool, len(documented))
	for _, f := range documented {
		present[f] = true
	}
	for _, f := range assumed {
		if !present[f] {
			missing = append(missing, f)
		}
	}
	return missing, nil
}
