package app

import (
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
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/spf13/cobra"
)

func (c *cli) updateCmd() *cobra.Command {
	root := &cobra.Command{Use: "update"}
	var channel string
	check := &cobra.Command{Use: "check", Short: "Check for a newer verified release", RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		release, err := fetchRelease(ctx, channel, "")
		if err != nil {
			return err
		}
		available := strings.TrimPrefix(release.Tag, "v") != Version
		result := map[string]any{"current": Version, "latest": release.Tag, "channel": channel, "update_available": available, "telemetry": false}
		status, title := cliui.StatusSuccess, "Agent Comms is up to date"
		if available {
			status, title = cliui.StatusInfo, "Update available"
		}
		return c.emitDocument("update.check", result, cliui.Document{
			Title: title, Status: status,
			Fields: []cliui.Field{{Label: "Current", Value: Version}, {Label: "Latest", Value: release.Tag}, {Label: "Channel", Value: channel}},
			Hint:   "Run agent-comms update apply to install a verified available release.",
		})
	}}
	check.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	var version string
	var yes, currentProjectOnly, skipProjectUpgrade bool
	allKnown := true
	apply := &cobra.Command{Use: "apply", Short: "Install a verified release and upgrade projects", RunE: func(cmd *cobra.Command, args []string) error {
		progress := c.progress()
		_ = progress.Start("Applying Agent Comms update")
		completed := false
		defer func() {
			if !completed {
				_ = progress.Stop(false, "Update did not complete")
			}
		}()
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		release, err := fetchRelease(ctx, channel, version)
		if err != nil {
			return err
		}
		result, err := installRelease(ctx, release)
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
		if currentProjectOnly {
			allKnown = false
		}
		knownRoots, rootsErr := c.knownProjectRoots(projectRoot)
		if rootsErr != nil {
			return rootsErr
		}
		if allKnown && len(knownRoots) == 0 {
			result["project_upgrade"] = map[string]any{"skipped": true, "reason": "no initialized projects are registered"}
		} else if allKnown || projectFound {
			upgradeResult, upgradeErr := c.handoffProjectUpgrade(ctx, result["installed"].(string), projectRoot, yes, allKnown)
			if upgradeErr != nil {
				// Preserve the handed-off binary's own classified error
				// (e.g. UPGRADE_REQUIRED is a normal, expected outcome, not
				// a failure) rather than collapsing every kind of error
				// into a generic UPGRADE_FAILED.
				var lifecycleErr *projectlifecycle.Error
				if errors.As(upgradeErr, &lifecycleErr) {
					return lifecycleErr
				}
				return &projectlifecycle.Error{
					Code:    projectlifecycle.CodeUpgradeFailed,
					Message: "binary updated successfully but project reconciliation failed: " + upgradeErr.Error(),
				}
			}
			result["project_upgrade"] = upgradeResult
		} else {
			result["project_upgrade"] = map[string]any{"skipped": true, "reason": "current directory is not an initialized project"}
		}
		completed = true
		_ = progress.Stop(true, "Update and project reconciliation completed")
		return c.emitUpdateApply(result)
	}}
	apply.Flags().StringVar(&channel, "channel", "stable", "stable or preview")
	apply.Flags().StringVar(&version, "version", "", "exact release tag")
	apply.Flags().BoolVarP(&yes, "yes", "y", false, "approve confirmation-required project migrations")
	apply.Flags().BoolVar(&allKnown, "all-known", true, "reconcile projects recorded in identity profiles")
	apply.Flags().BoolVar(&currentProjectOnly, "current-project-only", false, "reconcile only the current initialized project")
	apply.Flags().BoolVar(&skipProjectUpgrade, "skip-project-upgrade", false, "install the binary without reconciling projects")
	root.AddCommand(check, apply)
	return root
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
	info, err := os.Lstat(filepath.Join(absolute, store.Runtime))
	return absolute, err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
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
	backup := exe + ".previous"
	_ = os.Remove(backup)
	temporary, e := os.CreateTemp(filepath.Dir(exe), "."+filepath.Base(exe)+".update-*")
	if e != nil {
		return nil, e
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if e = temporary.Chmod(0o755); e == nil {
		_, e = temporary.Write(b)
	}
	if e == nil {
		e = temporary.Sync()
	}
	closeErr := temporary.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return nil, e
	}
	if e = os.Rename(exe, backup); e != nil {
		return nil, e
	}
	if e = os.Rename(temporaryPath, exe); e != nil {
		_ = os.Rename(backup, exe)
		return nil, e
	}
	if e = durablefs.SyncDirectory(filepath.Dir(exe)); e != nil {
		return nil, e
	}
	return map[string]any{"version": r.Tag, "installed": exe, "previous": backup, "verified": true}, nil
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
