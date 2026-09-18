package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/durablefs"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/releaseverify"
	"github.com/spf13/cobra"
)

func (c *cli) updateCmd() *cobra.Command {
	var channel, version string
	var yes, currentProjectOnly, skipProjectUpgrade bool
	allKnown := true
	update := &cobra.Command{
		Use:   "update",
		Short: "Check for and install a verified Agent Comms release",
		RunE: func(cmd *cobra.Command, args []string) error {
			fetchCtx, fetchCancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer fetchCancel()
			fetch := c.fetchReleaseFn
			if fetch == nil {
				fetch = fetchRelease
			}
			release, err := fetch(fetchCtx, channel, version)
			if err != nil {
				return err
			}
			latest := strings.TrimPrefix(release.Tag, "v")
			if latest == Version {
				return c.emitDocument("update.check", map[string]any{
					"current": Version, "latest": release.Tag, "channel": channel, "update_available": false,
				}, cliui.Document{
					Title: "Agent Comms is up to date", Status: cliui.StatusSuccess,
					Fields: []cliui.Field{{Label: "Version", Value: Version}, {Label: "Channel", Value: channel}},
				})
			}
			if !yes && !c.nonInteractive && !c.json {
				in := c.in
				if in == nil {
					in = os.Stdin
				}
				fmt.Fprintf(c.out, "Update available: v%s -> %s. Install? [y/N] ", Version, release.Tag)
				scanner := bufio.NewScanner(in)
				if !scanner.Scan() || !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
					return c.emitDocument("update.check", map[string]any{
						"current": Version, "latest": release.Tag, "channel": channel, "update_available": true, "installed": false,
					}, cliui.Document{
						Title: "Update available", Status: cliui.StatusInfo,
						Fields: []cliui.Field{{Label: "Current", Value: Version}, {Label: "Latest", Value: release.Tag}},
						Hint:   "Run agent-comms update again and answer y, or pass --yes, to install.",
					})
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			progress := c.progress()
			_ = progress.Start("Applying Agent Comms update")
			completed := false
			defer func() {
				if !completed {
					_ = progress.Stop(false, "Update did not complete")
				}
			}()
			install := c.installReleaseFn
			if install == nil {
				install = installRelease
			}
			result, err := install(ctx, release)
			if err != nil {
				return err
			}
			result["binary_updated"] = true
			if skipProjectUpgrade {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "requested by --skip-project-upgrade"}
				completed = true
				_ = progress.Stop(true, "Update installed")
				return c.emitUpdateApply(result)
			}
			projectRoot, projectFound := currentInitializedProject(c.project)
			effectiveAllKnown := allKnown
			if currentProjectOnly {
				effectiveAllKnown = false
			}
			knownRoots, rootsErr := c.knownProjectRoots(projectRoot)
			if rootsErr != nil {
				return rootsErr
			}
			if effectiveAllKnown && len(knownRoots) == 0 {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "no initialized projects are registered"}
			} else if effectiveAllKnown || projectFound {
				upgradeResult, upgradeErr := c.handoffProjectUpgrade(ctx, result["installed"].(string), projectRoot, yes, effectiveAllKnown)
				if upgradeErr != nil {
					details := map[string]any{
						"binary_updated":    true,
						"installed_version": result["version"],
						"previous_version":  result["previous"],
					}
					var lifecycleErr *projectlifecycle.Error
					if errors.As(upgradeErr, &lifecycleErr) {
						lifecycleErr.Details = details
						return lifecycleErr
					}
					return &projectlifecycle.Error{
						Code:    projectlifecycle.CodeUpgradeFailed,
						Message: "binary updated successfully but project reconciliation failed: " + upgradeErr.Error(),
						Details: details,
					}
				}
				result["project_upgrade"] = upgradeResult
			} else {
				result["project_upgrade"] = map[string]any{"skipped": true, "reason": "current directory is not an initialized project"}
			}
			completed = true
			_ = progress.Stop(true, "Update and project reconciliation completed")
			return c.emitUpdateApply(result)
		},
	}
	update.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	update.Flags().StringVar(&version, "version", "", "exact release tag")
	update.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt and approve confirmation-required project migrations")
	update.Flags().BoolVar(&allKnown, "all-known", true, "reconcile projects recorded in identity profiles")
	update.Flags().BoolVar(&currentProjectOnly, "current-project-only", false, "reconcile only the current initialized project")
	update.Flags().BoolVar(&skipProjectUpgrade, "skip-project-upgrade", false, "install the binary without reconciling projects")
	return update
}

func (c *cli) emitUpdateApply(result map[string]any) error {
	return c.emitDocument("update.apply", result, cliui.Document{
		Title: "Agent Comms updated", Status: cliui.StatusSuccess,
		Fields: []cliui.Field{
			{Label: "Version", Value: fmt.Sprint(result["version"])},
			{Label: "Installed", Value: fmt.Sprint(result["installed"])},
			{Label: "Previous", Value: fmt.Sprint(result["previous"])},
			{Label: "Verified", Value: fmt.Sprint(result["verified"])},
		},
		Hint: "Run agent-comms doctor in upgraded projects to confirm runtime and managed-file health.",
	})
}

// currentInitializedProject resolves explicit (or the current working
// directory, when empty) to an absolute path and reports whether it is a
// genuinely initialized project -- delegating entirely to
// initializedProject (internal/app/user_upgrade.go) so the two never drift
// out of sync the way they did before RFC 0035 (that duplicate, looser
// check was itself a second copy of the exact bug commit 13d8cf0 fixed).
func currentInitializedProject(explicit string) (string, bool) {
	root := explicit
	if root == "" {
		root, _ = os.Getwd()
	}
	if root == "" {
		return "", false
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	return absolute, initializedProject(absolute)
}

func (c *cli) handoffProjectUpgrade(ctx context.Context, executable, projectRoot string, yes, allKnown bool) (any, error) {
	arguments := []string{"project", "upgrade"}
	if projectRoot != "" {
		arguments = append(arguments, "--project", projectRoot)
	}
	if yes {
		arguments = append(arguments, "--yes")
	}
	if allKnown {
		arguments = append(arguments, "--all-known")
	}
	runner := c.handoffRunner
	if runner == nil {
		runner = runCommand
	}
	if c.json {
		arguments = append(arguments, "--json", "--non-interactive")
		var stdout, stderr bytes.Buffer
		if err := runner(ctx, executable, arguments, os.Stdin, &stdout, &stderr); err != nil {
			// The child's --json error envelope goes to its stderr (see
			// Run(), which encodes failures to the stderr writer), not
			// stdout. Parse it so the child's real classified error code
			// (e.g. UPGRADE_REQUIRED for a completely normal, expected
			// "needs confirmation" outcome) survives the handoff instead
			// of every failure collapsing into a generic UPGRADE_FAILED.
			var childEnvelope Envelope
			if decodeErr := json.Unmarshal(stderr.Bytes(), &childEnvelope); decodeErr == nil && childEnvelope.Error != nil {
				return nil, &projectlifecycle.Error{
					Code:    projectlifecycle.ErrorCode(childEnvelope.Error.Code),
					Message: childEnvelope.Error.Message,
				}
			}
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		var envelope Envelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			return nil, fmt.Errorf("decode upgraded binary response: %w", err)
		}
		if !envelope.OK {
			return nil, errors.New("upgraded binary did not verify the project")
		}
		return envelope.Result, nil
	}
	arguments = append(arguments, "--quiet")
	if err := runner(ctx, executable, arguments, os.Stdin, c.out, c.err); err != nil {
		return nil, err
	}
	return map[string]any{"verified": true, "all_known": allKnown}, nil
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}
type githubRelease struct {
	Tag        string         `json:"tag_name"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []releaseAsset `json:"assets"`
}

func fetchRelease(ctx context.Context, channel, version string) (githubRelease, error) {
	url := "https://api.github.com/repos/DhanushSantosh/AgentComms/releases"
	if version != "" {
		url += "/tags/" + version
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return githubRelease{}, e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return githubRelease{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	if version != "" {
		var r githubRelease
		e = json.NewDecoder(resp.Body).Decode(&r)
		return r, e
	}
	var all []githubRelease
	if e = json.NewDecoder(resp.Body).Decode(&all); e != nil {
		return githubRelease{}, e
	}
	for _, r := range all {
		if !r.Draft && (channel == "preview" || !r.Prerelease) {
			return r, nil
		}
	}
	return githubRelease{}, fmt.Errorf("no %s release is available", channel)
}
func installRelease(ctx context.Context, r githubRelease) (map[string]any, error) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	name := fmt.Sprintf("agent-comms-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
	urls := map[string]string{}
	for _, a := range r.Assets {
		urls[a.Name] = a.URL
	}
	for _, n := range []string{name, "checksums.txt", name + ".bundle"} {
		if urls[n] == "" {
			return nil, fmt.Errorf("release %s is missing %s", r.Tag, n)
		}
	}
	dir, e := os.MkdirTemp("", "agent-comms-update-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	for _, n := range []string{name, "checksums.txt", name + ".bundle"} {
		if e = download(ctx, urls[n], filepath.Join(dir, n)); e != nil {
			return nil, e
		}
	}
	checks, e := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if e != nil {
		return nil, e
	}
	expected := ""
	for _, line := range strings.Split(string(checks), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			expected = f[0]
		}
	}
	b, e := os.ReadFile(filepath.Join(dir, name))
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256(b)
	actual := hex.EncodeToString(h[:])
	if expected == "" || actual != expected {
		return nil, errors.New("release SHA-256 verification failed")
	}
	// Pure Go, no external cosign process required -- see
	// docs/rfcs/0015-cosign-free-release-verification.md.
	if e := releaseverify.VerifyBlob(
		filepath.Join(dir, name), filepath.Join(dir, name+".bundle"),
		"https://token.actions.githubusercontent.com",
		`^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/`,
	); e != nil {
		return nil, fmt.Errorf("release verification failed: %w", e)
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	exe, backup, e := replaceExecutable(exe, b)
	if e != nil {
		return nil, e
	}
	return map[string]any{"version": r.Tag, "installed": exe, "previous": backup, "verified": true}, nil
}

// replaceExecutable atomically swaps the file at exePath for b, keeping
// the old one as "<path>.previous". If exePath is a symlink (the `agc`
// alias, RFC 0030), it follows it and replaces the real target -- an
// updater invoked through a symlink must update the target, and the
// by-name `agc` symlink then keeps resolving to the new binary. Returns
// the real installed path and the backup path.
func replaceExecutable(exePath string, b []byte) (installed, backup string, err error) {
	if resolved, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
		exePath = resolved
	}
	backup = exePath + ".previous"
	_ = os.Remove(backup)
	temporary, err := os.CreateTemp(filepath.Dir(exePath), "."+filepath.Base(exePath)+".update-*")
	if err != nil {
		return "", "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o755); err == nil {
		_, err = temporary.Write(b)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", "", err
	}
	if err = os.Rename(exePath, backup); err != nil {
		return "", "", err
	}
	if err = os.Rename(temporaryPath, exePath); err != nil {
		_ = os.Rename(backup, exePath)
		return "", "", err
	}
	if err = durablefs.SyncDirectory(filepath.Dir(exePath)); err != nil {
		return "", "", err
	}
	return exePath, backup, nil
}

func download(ctx context.Context, url, path string) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return e
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
