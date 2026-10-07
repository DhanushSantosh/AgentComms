package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/spf13/cobra"
)

func (c *cli) agentCmd() *cobra.Command {
	root := &cobra.Command{Use: "agent"}
	var display, ptype string
	reg := &cobra.Command{Use: "register", Short: "Register a governed identity", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		provider, _ := cmd.Flags().GetString("provider")
		principal := model.PrincipalType(strings.ToUpper(ptype))
		// RFC 0039: an AGENT's ID names its provider. --provider is the
		// primary input and --id optional, defaulting to "<provider>" or
		// "<provider>-2" when that is taken, so the common case needs no
		// ID at all. HUMAN principals are exempt and keep --id required.
		if principal == model.PrincipalAgent {
			resolved, resolveErr := c.resolveAgentID(id, provider)
			if resolveErr != nil {
				return resolveErr
			}
			id = resolved
		} else if strings.TrimSpace(id) == "" {
			return errors.New("agent register: --id is required for a HUMAN principal")
		}
		if id != c.actor {
			can, e := c.svc.CanSponsorRegistration(c.actor)
			if e != nil {
				return e
			}
			if !can {
				return fmt.Errorf("agent register: registering a different id requires an active orchestrator or human principal (actor: %s)", c.actor)
			}
		}
		v, e := c.svc.Register(id, display, principal)
		if e != nil {
			return e
		}
		// Surface the profile name and project root so the user knows
		// where their identity was persisted.
		type registerResult struct {
			model.Event
			ProfileName   string `json:"profile_name"`
			ProjectRoot   string `json:"project_root"`
			ActorSource   string `json:"actor_source"`
			ActiveProfile string `json:"active_profile"`
		}
		actorSource := "self-registration"
		if id != c.actor {
			actorSource = "sponsored by " + c.actor
		}
		cfg, cfgErr := c.svc.Store.Config()
		if cfgErr != nil {
			return c.emit("agent.register", v)
		}
		result := registerResult{
			Event:       v,
			ProfileName: cfg.ProjectID + ":" + id,
			ProjectRoot: c.svc.Store.Root,
			ActorSource: actorSource,
			// UX-03: registering a different id never switches the
			// session's own active profile -- the CLI keeps writing as
			// whoever ran this command (c.actor). Reported explicitly
			// instead of leaving it to be inferred, because acting on that
			// wrong assumption is exactly what produced the "active
			// principal required" surprise the audit reproduced: a script
			// that registers an agent and immediately tries to act as it
			// (still unactivated, and the session is still signed as the
			// sponsor either way) fails for two independent reasons at
			// once, easy to conflate into one.
			ActiveProfile: c.actor,
		}
		return c.emitDocument("agent.register", result, cliui.Document{
			Title:  "Agent registered · PENDING, awaiting activation",
			Status: cliui.StatusSuccess,
			Fields: []cliui.Field{
				{Label: "Agent", Value: id},
				{Label: "Status", Value: "PENDING"},
				{Label: "Registered by", Value: c.actor},
				{Label: "Active profile", Value: c.actor + " (unchanged -- registering never switches it)"},
				{Label: "Profile", Value: result.ProfileName},
				{Label: "Project", Value: result.ProjectRoot},
				{Label: "Sequence", Value: fmt.Sprint(v.Sequence)},
			},
			Hint: fmt.Sprintf("An active orchestrator or human principal must run `agent-comms agent activate --id %s --role <role> --scope <scope>` before %s can act.", id, id),
		})
	}}
	reg.Flags().String("id", "", "principal ID (optional for AGENT: defaults to the provider name)")
	reg.Flags().String("provider", "", "AI provider backing this agent: "+strings.Join(model.KnownProviders(), ", ")+", or one registered with `provider add`")
	// --id is no longer required: for an AGENT it is derived from
	// --provider (RFC 0039). RunE enforces that one of the two is present,
	// and that a HUMAN principal still supplies an --id, which cobra's
	// blanket "required" could not express.
	reg.Flags().StringVar(&display, "display-name", "", "display name")
	reg.Flags().StringVar(&ptype, "principal-type", "AGENT", "HUMAN or AGENT")
	var role string
	var caps, scopes []string
	act := &cobra.Command{Use: "activate", Short: "Activate an identity with a role and scopes", RunE: func(cmd *cobra.Command, args []string) error {
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
	rotate := &cobra.Command{Use: "rotate-key", Short: "Rotate this identity's signing key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
	rename := &cobra.Command{Use: "rename", Short: "Rename an identity's display name", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
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
	list := &cobra.Command{Use: "list", Short: "List governed identities", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		headers := []string{"ID", "STATUS", "ROLE", "TYPE", "SCOPES", "UPDATED"}
		rows := make([][]string, 0, len(st.Agents))
		ids := model.SortedIDsBySequence(st.Agents, func(a model.Agent) uint64 { return a.UpdatedSequence })
		for _, id := range ids {
			a := st.Agents[id]
			rows = append(rows, []string{id, a.Status, string(a.Role), string(a.PrincipalType), strings.Join(a.Scopes, ","), formatEntityTime(a.UpdatedAt)})
		}
		return c.emitTableFullOrdered("agent.list", st.Agents, ids, headers, []int{3, 1, 0, 2, 4, 5}, "", rows)
	}}
	show := c.entityShow("agent", func(st model.State, id string) (any, []cliui.Field, bool) {
		a, ok := st.Agents[id]
		if !ok {
			return nil, nil, false
		}
		provider := "-"
		if a.PrincipalType == model.PrincipalAgent {
			if name, ok := model.RecognizedProviders(st).ProviderOf(id); ok {
				provider = name
			}
		}
		return a, []cliui.Field{
			{Label: "Status", Value: a.Status}, {Label: "Role", Value: string(a.Role)},
			{Label: "Provider", Value: provider},
			{Label: "Type", Value: string(a.PrincipalType)}, {Label: "Scopes", Value: strings.Join(a.Scopes, ",")},
			{Label: "Created", Value: formatEntityTime(a.CreatedAt)},
			{Label: "Updated", Value: formatEntityTime(a.UpdatedAt)},
		}, true
	})
	root.AddCommand(reg, act, switchRoleCmd, suspend, rotate, elevate, rename, revoke, deleteAgent, list, show)
	return root
}

// resolveAgentID applies RFC 0039's rules to the --id/--provider pair.
//
// Either flag alone is enough: --provider derives the ID, --id implies its
// provider. Given both, they must agree, because silently preferring one
// would make the other a lie in the receipt the caller reads back.
func (c *cli) resolveAgentID(id, provider string) (string, error) {
	id = strings.TrimSpace(id)
	provider = strings.ToLower(strings.TrimSpace(provider))
	state, stateErr := c.svc.State()
	if stateErr != nil {
		return "", stateErr
	}
	// RFC 0050: the accepted providers are the built-ins plus the project's
	// registered ones. A missing provider can be registered on the spot by
	// an actor allowed to, after an explicit yes.
	providers := model.RegistrableProviders(state)
	if provider != "" && !providers.Has(provider) {
		if err := c.offerProviderRegistration(state, provider); err != nil {
			return "", err
		}
		if state, stateErr = c.svc.State(); stateErr != nil {
			return "", stateErr
		}
		providers = model.RegistrableProviders(state)
	}
	if id == "" {
		if provider == "" {
			return "", fmt.Errorf("agent register: --provider is required for an AGENT (one of: %s), or pass --id naming it",
				strings.Join(providers.Names(), ", "))
		}
		return model.DefaultAgentActorID(provider, func(candidate string) bool {
			_, taken := state.Agents[candidate]
			return taken
		}), nil
	}
	if err := providers.ValidateAgentActorID(id); err != nil {
		// "gemini-main" names a provider the project lacks; a bare name
		// like "reviewer" keeps RFC 0039's suggestion.
		name, _, hyphenated := strings.Cut(id, "-")
		if !hyphenated || provider != "" || providers.Has(name) || model.ValidateProviderName(name) != nil {
			return "", err
		}
		if offerErr := c.offerProviderRegistration(state, name); offerErr != nil {
			return "", offerErr
		}
		if state, stateErr = c.svc.State(); stateErr != nil {
			return "", stateErr
		}
		providers = model.RegistrableProviders(state)
		if err = providers.ValidateAgentActorID(id); err != nil {
			return "", err
		}
	}
	if provider != "" {
		if actual, _ := providers.ProviderOf(id); actual != provider {
			return "", fmt.Errorf("agent register: --id %q names provider %q, which contradicts --provider %q",
				id, actual, provider)
		}
	}
	return id, nil
}
