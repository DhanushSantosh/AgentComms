package app

import (
	"fmt"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/spf13/cobra"
)

func (c *cli) agentCmd() *cobra.Command {
	root := &cobra.Command{Use: "agent"}
	var display, ptype string
	reg := &cobra.Command{Use: "register", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id != c.actor {
			can, e := c.svc.CanSponsorRegistration(c.actor)
			if e != nil {
				return e
			}
			if !can {
				return fmt.Errorf("agent register: registering a different id requires an active orchestrator or human principal (actor: %s)", c.actor)
			}
		}
		v, e := c.svc.Register(id, display, model.PrincipalType(strings.ToUpper(ptype)))
		if e != nil {
			return e
		}
		// Surface the profile name and project root so the user knows
		// where their identity was persisted.
		type registerResult struct {
			model.Event
			ProfileName string `json:"profile_name"`
			ProjectRoot string `json:"project_root"`
			ActorSource string `json:"actor_source"`
		}
		actorSource := "self-registration"
		if id != c.actor {
			actorSource = "sponsored by " + c.actor
		}
		cfg, cfgErr := c.svc.Store.Config()
		if cfgErr != nil {
			return c.emit("agent.register", v)
		}
		return c.emit("agent.register", registerResult{
			Event:       v,
			ProfileName: cfg.ProjectID + ":" + id,
			ProjectRoot: c.svc.Store.Root,
			ActorSource: actorSource,
		})
	}}
	reg.Flags().String("id", "", "principal ID")
	_ = reg.MarkFlagRequired("id")
	reg.Flags().StringVar(&display, "display-name", "", "display name")
	reg.Flags().StringVar(&ptype, "principal-type", "AGENT", "HUMAN or AGENT")
	var role string
	var caps, scopes []string
	act := &cobra.Command{Use: "activate", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "agent.activate", id, model.AgentActivated{Role: model.Role(role), Capabilities: caps, Scopes: scopes})
		if e != nil {
			return e
		}
		return c.emit("agent.activate", v)
	}}
	act.Flags().String("id", "", "principal ID")
	_ = act.MarkFlagRequired("id")
	// No default: RoleAgent no longer exists (see RFC 0018) and there is no
	// other generic role to fall back to -- ORCHESTRATOR or any freeform
	// custom label (e.g. Frontend-Architect, Tester) must be named
	// explicitly.
	act.Flags().StringVar(&role, "role", "", "role: ORCHESTRATOR or any custom label")
	_ = act.MarkFlagRequired("role")
	act.Flags().StringSliceVar(&caps, "capability", nil, "capability (repeatable or comma-separated)")
	act.Flags().StringSliceVar(&scopes, "scope", nil, "scope (repeatable or comma-separated)")
	var switchRole string
	switchRoleCmd := &cobra.Command{Use: "switch-role", Args: cobra.NoArgs, Short: "Switch your own role (self-service; never OWNER)", RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.Execute(c.actor, "agent.switch-role", c.actor, model.AgentRoleSwitched{Role: model.Role(switchRole)})
		if e != nil {
			return e
		}
		return c.emit("agent.switch-role", v)
	}}
	switchRoleCmd.Flags().StringVar(&switchRole, "role", "", "new role: ORCHESTRATOR or any custom label")
	_ = switchRoleCmd.MarkFlagRequired("role")
	suspend := simpleStatus(c, "agent", "suspend")
	var revokeReason string
	revoke := payloadStatus(c, "agent", "revoke", func(string) any {
		return model.RuntimeStatusChanged{Reason: revokeReason}
	})
	revoke.Flags().StringVar(&revokeReason, "reason", "", "revocation reason")
	var deleteReason string
	deleteAgent := payloadStatus(c, "agent", "delete", func(string) any {
		return model.AgentDeleted{Reason: deleteReason}
	})
	deleteAgent.Flags().StringVar(&deleteReason, "reason", "", "auditable deletion reason")
	_ = deleteAgent.MarkFlagRequired("reason")
	rotate := &cobra.Command{Use: "rotate-key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.RotateKey(c.actor)
		if e != nil {
			return e
		}
		return c.emit("agent.rotate-key", v)
	}}
	// elevate-key is deliberately CLI-only: it exists to prove a human typed
	// a passphrase into a real terminal, which is meaningless to expose over
	// MCP (an agent connection has no interactive terminal to answer the
	// prompt with in the first place). See docs/governance.md for what this
	// closes: a locally-running agent can otherwise sign anything with the
	// primary key exactly as if it were the human, indistinguishably.
	elevate := &cobra.Command{Use: "elevate-key", Args: cobra.NoArgs, Short: "Register a passphrase-protected key for sensitive identity and HUMAN-approval actions", RunE: func(cmd *cobra.Command, args []string) error {
		passphrase, e := promptNewPassphrase(c.actor)
		if e != nil {
			return e
		}
		v, e := c.svc.ElevateKey(c.actor, passphrase)
		if e != nil {
			return e
		}
		return c.emit("agent.elevate-key", v)
	}}
	var newDisplayName string
	rename := &cobra.Command{Use: "rename", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "agent.rename", id, model.AgentRenamed{DisplayName: newDisplayName})
		if e != nil {
			return e
		}
		return c.emit("agent.rename", v)
	}}
	rename.Flags().String("id", "", "principal ID")
	_ = rename.MarkFlagRequired("id")
	rename.Flags().StringVar(&newDisplayName, "display-name", "", "new display name")
	_ = rename.MarkFlagRequired("display-name")
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		headers := []string{"ID", "STATUS", "ROLE", "TYPE", "SCOPES"}
		rows := make([][]string, 0, len(st.Agents))
		for _, id := range service.SortedKeys(st.Agents) {
			a := st.Agents[id]
			rows = append(rows, []string{id, a.Status, string(a.Role), string(a.PrincipalType), strings.Join(a.Scopes, ",")})
		}
		return c.emitTable("agent.list", st.Agents, headers, rows)
	}}
	root.AddCommand(reg, act, switchRoleCmd, suspend, rotate, elevate, rename, revoke, deleteAgent, list)
	return root
}
