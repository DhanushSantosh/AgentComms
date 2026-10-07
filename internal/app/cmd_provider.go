package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/spf13/cobra"
)

func (c *cli) providerCmd() *cobra.Command {
	root := &cobra.Command{Use: "provider", Short: "Manage the agent providers this project accepts"}
	var displayName, description, reason string
	add := &cobra.Command{
		Use:     "add <name>",
		Aliases: []string{"register"},
		Short:   "Register a provider so agents can use <name> or <name>-<suffix> IDs",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			event, err := c.svc.Execute(c.actor, "provider.register", name, model.ProviderRegistered{DisplayName: displayName, Description: description})
			if err != nil {
				return err
			}
			return c.emitDocument("provider.register", event, cliui.Document{
				Title:  "Provider registered",
				Status: cliui.StatusSuccess,
				Fields: []cliui.Field{
					{Label: "Provider", Value: name},
					{Label: "Registered by", Value: c.actor},
					{Label: "Sequence", Value: fmt.Sprint(event.Sequence)},
				},
				Hint: fmt.Sprintf("Register an agent with `agent-comms agent register --provider %s`.", name),
			})
		},
	}
	add.Flags().StringVar(&displayName, "display-name", "", "display name, for example \"Google Gemini CLI\"")
	add.Flags().StringVar(&description, "description", "", "description")

	retire := &cobra.Command{
		Use:   "retire <name>",
		Short: "Stop new agent registrations under a registered provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			st, err := c.svc.State()
			if err != nil {
				return err
			}
			views, _ := model.ProviderListings(st)
			event, err := c.svc.Execute(c.actor, "provider.retire", name, model.ProviderRetired{Reason: reason})
			if err != nil {
				return err
			}
			active := views[name].ActiveAgents
			fields := []cliui.Field{
				{Label: "Provider", Value: name},
				{Label: "Retired by", Value: c.actor},
				{Label: "Sequence", Value: fmt.Sprint(event.Sequence)},
			}
			hint := ""
			if len(active) > 0 {
				fields = append(fields, cliui.Field{Label: "Still active", Value: strings.Join(active, ", ")})
				hint = "Existing agents keep working. Suspend or revoke them separately if that is the intent."
			}
			return c.emitDocument("provider.retire", event, cliui.Document{
				Title: "Provider retired", Status: cliui.StatusSuccess, Fields: fields, Hint: hint,
			})
		},
	}
	retire.Flags().StringVar(&reason, "reason", "", "why the provider is retired")
	_ = retire.MarkFlagRequired("reason")

	list := &cobra.Command{Use: "list", Short: "List built-in and registered providers", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		st, err := c.svc.State()
		if err != nil {
			return err
		}
		views, order := model.ProviderListings(st)
		rows := make([][]string, 0, len(order))
		for _, name := range order {
			view := views[name]
			kind, added := "registered", formatEntityTime(view.CreatedAt)
			if view.BuiltIn {
				kind, added = "built-in", "-"
			}
			rows = append(rows, []string{name, view.Status, kind, fmt.Sprint(len(view.ActiveAgents)), view.DisplayName, added})
		}
		return c.emitTableFullOrdered("provider.list", views, order,
			[]string{"NAME", "STATUS", "KIND", "ACTIVE AGENTS", "DISPLAY NAME", "ADDED"}, []int{0, 1, 2, 3, 5, 4}, "", rows)
	}}

	show := &cobra.Command{Use: "show <name>", Short: "Show one provider", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.ToLower(strings.TrimSpace(args[0]))
		st, err := c.svc.State()
		if err != nil {
			return err
		}
		views, _ := model.ProviderListings(st)
		view, ok := views[name]
		if !ok {
			return fmt.Errorf("provider %q is not registered for this project", name)
		}
		fields := []cliui.Field{{Label: "Status", Value: view.Status}}
		if view.BuiltIn {
			fields = append(fields, cliui.Field{Label: "Kind", Value: "built-in"})
		} else {
			fields = append(fields,
				cliui.Field{Label: "Kind", Value: "registered"},
				cliui.Field{Label: "Display name", Value: view.DisplayName},
				cliui.Field{Label: "Description", Value: view.Description},
				cliui.Field{Label: "Registered by", Value: view.AddedBy},
				cliui.Field{Label: "Created", Value: formatEntityTime(view.CreatedAt)},
				cliui.Field{Label: "Updated", Value: formatEntityTime(view.UpdatedAt)})
			if view.Status == model.ProviderStatusRetired {
				fields = append(fields, cliui.Field{Label: "Retired by", Value: view.RetiredBy}, cliui.Field{Label: "Reason", Value: view.RetireReason})
			}
		}
		fields = append(fields, cliui.Field{Label: "Active agents", Value: strings.Join(view.ActiveAgents, ", ")})
		return c.emitDocument("provider.show", view, cliui.Document{Title: "Provider " + name, Status: cliui.StatusInfo, Fields: fields})
	}}

	root.AddCommand(add, retire, list, show)
	return root
}

// canManageProviders reports whether the CLI's actor may register or retire
// providers: the owner or an active orchestrator, human or agent (RFC 0050).
func canManageProviders(st model.State, actor string) bool {
	principal, ok := st.Agents[actor]
	return ok && principal.Status == "ACTIVE" &&
		(principal.Role == model.RoleOwner || principal.Role == model.RoleOrchestrator)
}

// offerProviderRegistration handles an agent registration that names a
// provider the project does not accept. When the actor may manage providers
// and a prompt is allowed, it offers to register the provider first and
// returns nil once it has. Otherwise it returns an error naming the exact
// command to run.
func (c *cli) offerProviderRegistration(st model.State, name string) error {
	accepted := strings.Join(model.RegistrableProviders(st).Names(), ", ")
	if err := model.ValidateProviderName(name); err != nil {
		return fmt.Errorf("agent register: unknown provider %q (%v); known providers: %s", name, err, accepted)
	}
	verb, command := "register", "agent-comms provider add "+name
	if existing, ok := st.Providers[name]; ok && existing.Status == model.ProviderStatusRetired {
		verb = "reactivate"
	}
	notRegistered := fmt.Errorf("agent register: provider %q is not registered for this project; %s it with `%s` (known providers: %s)",
		name, verb, command, accepted)
	if c.nonInteractive || c.json || !canManageProviders(st, c.actor) {
		return notRegistered
	}
	in := c.in
	if in == nil {
		in = os.Stdin
	}
	prompt := "Register"
	if verb == "reactivate" {
		prompt = "Reactivate"
	}
	fmt.Fprintf(c.out, "Provider %q is not registered for this project. %s it now? [y/N] ", name, prompt)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() || !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
		return errors.New("agent register: cancelled; " + notRegistered.Error()[len("agent register: "):])
	}
	if _, err := c.svc.Execute(c.actor, "provider.register", name, model.ProviderRegistered{}); err != nil {
		return fmt.Errorf("agent register: registering provider %q: %w", name, err)
	}
	return nil
}
