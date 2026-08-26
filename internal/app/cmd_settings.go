package app

import (
	"errors"
	"sort"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
	"github.com/spf13/cobra"
)

func (c *cli) profileCmd() *cobra.Command {
	root := &cobra.Command{Use: "profile"}
	current := &cobra.Command{Use: "current", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
	}}
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
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
	use := &cobra.Command{Use: "use", RunE: func(cmd *cobra.Command, args []string) error {
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
		return c.emit("profile.use", map[string]any{"active": name, "session_scoped": sessionID != ""})
	}}
	use.Flags().StringVar(&name, "name", "", "profile name")
	root.AddCommand(current, list, use)
	return root
}
func (c *cli) configCmd() *cobra.Command {
	return &cobra.Command{Use: "config", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		p, e := c.svc.Store.Config()
		if e != nil {
			return e
		}
		return c.emit("config", map[string]any{"user": u, "project": p, "precedence": []string{"flags", "environment", "project", "user", "defaults"}})
	}}
}
func (c *cli) themeCmd() *cobra.Command {
	root := &cobra.Command{Use: "theme", Short: "Manage UI theme"}
	var name string
	set := &cobra.Command{Use: "set", RunE: func(cmd *cobra.Command, args []string) error {
		u, e := identity.LoadUserConfig()
		if e != nil {
			return e
		}
		u.Theme = name
		if e = identity.SaveUserConfig(u); e != nil {
			return e
		}
		return c.emit("theme.set", map[string]string{"theme": name})
	}}
	set.Flags().StringVar(&name, "name", "", "theme (auto, dark, high-contrast)")
	_ = set.MarkFlagRequired("name")
	root.AddCommand(set)
	return root
}
