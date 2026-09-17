package app

import (
	"errors"
	"sort"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/spf13/cobra"
)

func (c *cli) profileCmd() *cobra.Command {
	root := &cobra.Command{Use: "profile"}
	current := &cobra.Command{Use: "current", Args: cobra.NoArgs, Short: "Show the active signing profile", RunE: func(cmd *cobra.Command, args []string) error {
		// projectOptional (RFC 0027 section 12): inside a project the fully
		// resolved actor (host binding, session scope, ...) is available;
		// outside one, fall back to the active profile in user config so
		// the command still answers.
		if c.actorResolution.Actor != "" {
			return c.emitDocument("profile.current", c.actorResolution, cliui.Document{
				Title:  "Active profile",
				Status: cliui.StatusInfo,
				Fields: []cliui.Field{
					{Label: "Actor", Value: c.actorResolution.Actor},
					{Label: "Profile", Value: c.actorResolution.Profile},
					{Label: "Source", Value: c.actorResolution.Source},
					{Label: "Project", Value: c.actorResolution.ProjectID},
				},
			})
		}
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		sessionID, _ := sessionbind.Capture()
		activeName := u.ActiveProfileFor(sessionID)
		active := u.Profiles[activeName]
		result := map[string]any{"active": activeName, "profile": active, "session_scoped": sessionID != ""}
		return c.emitDocument("profile.current", result, cliui.Document{
			Title:  "Active profile",
			Status: cliui.StatusInfo,
			Fields: []cliui.Field{
				{Label: "Profile", Value: activeName},
				{Label: "Actor", Value: active.Actor},
				{Label: "Project", Value: active.ProjectID},
				{Label: "Session scoped", Value: map[bool]string{true: "yes", false: "no"}[sessionID != ""]},
			},
		})
	}}
	list := &cobra.Command{Use: "list", Short: "List local signing profiles", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		sessionID, _ := sessionbind.Capture()
		// session_scoped tells the caller (human or agent) whether "active"
		// below reflects its own isolated session or the shared,
		// machine-wide legacy default -- see RFC 0016.
		result := map[string]any{
			"active": u.ActiveProfileFor(sessionID), "profiles": u.Profiles,
			"session_scoped": sessionID != "",
		}
		names := make([]string, 0, len(u.Profiles))
		for name := range u.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		active := u.ActiveProfileFor(sessionID)
		rows := make([][]string, 0, len(names))
		for _, name := range names {
			profile := u.Profiles[name]
			marker := ""
			if name == active {
				marker = "active"
			}
			rows = append(rows, []string{name, profile.Actor, profile.ProjectID, profile.HostLabel, marker})
		}
		return c.emitTable("profile.list", result, []string{"PROFILE", "ACTOR", "PROJECT", "HOST", "STATE"}, rows)
	}}
	var name string
	use := &cobra.Command{Use: "use", Short: "Select the active signing profile", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		if _, ok := u.Profiles[name]; !ok {
			return errors.New("profile not found")
		}
		sessionID, _ := sessionbind.Capture()
		// Scoped to this exact session when one is recognized (an agent's
		// own conversation, most commonly) rather than the legacy
		// machine-wide field every other process would otherwise inherit
		// too -- see RFC 0016.
		u.SetActiveProfileFor(sessionID, name)
		if e = identity.SaveUserConfig(u); e != nil {
			return e
		}
		result := map[string]any{"active": name, "session_scoped": sessionID != ""}
		return c.emitDocument("profile.use", result, cliui.Document{
			Title: "Profile selected", Status: cliui.StatusSuccess,
			Fields: []cliui.Field{{Label: "Profile", Value: name}, {Label: "Session scoped", Value: map[bool]string{true: "yes", false: "no"}[sessionID != ""]}},
		})
	}}
	use.Flags().StringVar(&name, "name", "", "profile name")
	root.AddCommand(current, list, use)
	return root
}
func (c *cli) configCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "config",
		Short: "Inspect and set user and project configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			u, e := identity.LoadUserConfig()
			if e != nil {
				return e
			}
			result := map[string]any{"user": u, "precedence": []string{"flags", "environment", "project", "user", "defaults"}}
			fields := []cliui.Field{
				{Label: "Active profile", Value: u.ActiveProfile}, {Label: "Theme", Value: u.Theme}, {Label: "Update channel", Value: u.UpdateChannel},
			}
			// projectOptional: c.svc is nil outside an initialized project
			// (RFC 0027 section 12). Report user config only in that case.
			if c.svc != nil {
				p, cfgErr := c.svc.Store.Config()
				if cfgErr != nil {
					return cfgErr
				}
				result["project"] = p
				state, stateErr := c.svc.State()
				if stateErr == nil {
					result["invocation_policies"] = state.InvocationPolicies
				}
				result["limits"] = map[string]any{
					"max_runtime_concurrency": controlplane.MaxRuntimeConcurrency,
					"max_delivery_attempts":   controlplane.MaxDeliveryAttempts,
					"max_invocation_bytes":    controlplane.MaxInvocationBytes,
					"max_invocation_ttl":      controlplane.MaxInvocationTTL.String(),
				}
				fields = append([]cliui.Field{
					{Label: "Project", Value: p.ProjectID}, {Label: "Runtime mode", Value: p.RuntimeMode},
				}, fields...)
			} else {
				result["project"] = nil
			}
			return c.emitDocument("config", result, cliui.Document{
				Title: "Resolved configuration", Status: cliui.StatusInfo, Fields: fields,
				Hint: "Use --details for sources, control-plane limits, and the complete resolved configuration.",
			})
		},
	}
	var themeName string
	theme := &cobra.Command{
		Use:   "theme <auto|dark|high-contrast>",
		Short: "Set the UI theme for this user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			themeName = args[0]
			u, e := identity.LoadUserConfig()
			if e != nil {
				return e
			}
			u.Theme = themeName
			if e = identity.SaveUserConfig(u); e != nil {
				return e
			}
			return c.emitDocument("config.theme", map[string]string{"theme": themeName}, cliui.Document{
				Title: "Theme updated", Status: cliui.StatusSuccess, Fields: []cliui.Field{{Label: "Theme", Value: themeName}},
			})
		},
	}
	root.AddCommand(theme)
	return root
}
