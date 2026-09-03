package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/spf13/cobra"
)

func (c *cli) taskCmd() *cobra.Command {
	root := &cobra.Command{Use: "task"}
	var title, summary, repo, branch, worktree, external, risk string
	var resources []string
	create := &cobra.Command{Use: "create", Short: "Create a tracked task", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("task-%d", time.Now().UnixNano())
		}
		v, e := c.svc.Execute(c.actor, "task.create", id, model.TaskCreated{Title: title, Summary: summary, Repository: repo, Branch: branch, Worktree: worktree, Resources: resources, ExternalRef: external, Risk: risk})
		if e != nil {
			return e
		}
		return c.emit("task.create", v)
	}}
	create.Flags().String("id", "", "task ID (auto-generated if omitted)")
	create.Flags().StringVar(&title, "title", "", "title")
	create.Flags().StringVar(&summary, "summary", "", "summary")
	create.Flags().StringVar(&repo, "repository", "local", "repository")
	create.Flags().StringVar(&branch, "branch", "", "branch")
	create.Flags().StringVar(&worktree, "worktree", "", "worktree path")
	create.Flags().StringSliceVar(&resources, "resource", nil, "write resource")
	create.Flags().StringVar(&external, "external-ref", "", "external reference")
	create.Flags().StringVar(&risk, "risk", "ROUTINE", "risk tier")
	var to string
	var offerTTL time.Duration
	offer := &cobra.Command{Use: "offer", Short: "Offer a task to another principal", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		v, e := c.svc.Execute(c.actor, "task.offer", id, model.TaskOffered{To: to, ExpiresAt: time.Now().UTC().Add(offerTTL)})
		if e != nil {
			return e
		}
		return c.emit("task.offer", v)
	}}
	offer.Flags().String("id", "", "task ID")
	_ = offer.MarkFlagRequired("id")
	offer.Flags().StringVar(&to, "to", "", "principal")
	offer.Flags().DurationVar(&offerTTL, "expires-in", time.Hour, "offer validity")
	var leaseDuration time.Duration
	var claimRepo, claimWorktree string
	claim := &cobra.Command{Use: "claim", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		lease := time.Now().UTC().Add(leaseDuration)
		// --repo and --worktree are two names for the same lock, kept as
		// separate flags because that's how the original working-directory-
		// lock request (docs/agent-comms-feedback.md #1) phrased it ("task
		// claim <task-id> --repo <path> --branch <name>"). --repo used to be
		// declared and parsed but never actually reached TaskClaimed's
		// Worktree field -- passing it silently acquired no lock at all
		// despite its own help text ("acquires a working-directory lock").
		worktree := claimWorktree
		if worktree == "" {
			worktree = claimRepo
		}
		payload := model.TaskClaimed{LeaseUntil: lease, Worktree: worktree}
		v, e := c.svc.Execute(c.actor, "task.claim", id, payload)
		if e != nil {
			return e
		}
		return c.emitDocument("task.claim", v, cliui.Document{
			Title:  "Task claimed",
			Status: cliui.StatusSuccess,
			Fields: []cliui.Field{
				{Label: "Task", Value: id},
				{Label: "Actor", Value: c.actor},
				{Label: "Lease until", Value: lease.Format(time.RFC3339)},
				{Label: "Worktree", Value: worktree},
			},
			Hint: "Start the task when work begins, then renew the lease with progress before it expires.",
		})
	}}
	claim.Short = "Claim a task and acquire its working-directory lock"
	claim.Flags().String("id", "", "task ID")
	_ = claim.MarkFlagRequired("id")
	claim.Flags().DurationVar(&leaseDuration, "duration", 4*time.Hour, "lease duration")
	claim.Flags().StringVar(&claimWorktree, "worktree", "", "worktree path (acquires the working-directory lock)")
	// RFC 0027 section 11: --worktree is canonical; --repo is a hidden
	// deprecated alias kept one release for shell history.
	claim.Flags().StringVar(&claimRepo, "repo", "", "deprecated alias for --worktree")
	_ = claim.Flags().MarkHidden("repo")
	start := simpleStatus(c, "task", "start")
	var progress string
	renew := payloadStatus(c, "task", "renew", func(string) any { return model.TaskRenewed{Progress: progress} })
	renew.Flags().StringVar(&progress, "progress", "", "progress summary")
	block := statusWithSummary(c, "task", "block")
	review := statusWithSummary(c, "task", "review")
	complete := statusWithSummary(c, "task", "complete")
	cancel := statusWithSummary(c, "task", "cancel")
	var handTo, handSummary string
	handoff := &cobra.Command{Use: "handoff", Short: "Hand off or accept a task", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		accept, _ := cmd.Flags().GetBool("accept")
		typ := "task.handoff"
		var p any = model.TaskHandoff{To: handTo, Summary: handSummary}
		if accept {
			typ = "task.handoff.accept"
			p = model.TaskStatus{Summary: handSummary}
		}
		v, e := c.svc.Execute(c.actor, typ, id, p)
		if e != nil {
			return e
		}
		return c.emit(typ, v)
	}}
	handoff.Flags().String("id", "", "task ID")
	_ = handoff.MarkFlagRequired("id")
	handoff.Flags().StringVar(&handTo, "to", "", "handoff target")
	handoff.Flags().StringVar(&handSummary, "summary", "", "handoff summary")
	handoff.Flags().Bool("accept", false, "accept pending handoff")
	takeover := statusWithSummary(c, "task", "takeover")
	list := &cobra.Command{Use: "list", Short: "List tasks", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		ids := make([]string, 0, len(st.Tasks))
		for id := range st.Tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			task := st.Tasks[id]
			rows = append(rows, []string{id, task.Title, task.Status, task.Owner, task.Branch})
		}
		return c.emitTable("task.list", st.Tasks, []string{"ID", "TITLE", "STATUS", "OWNER", "BRANCH"}, rows)
	}}
	var lockWorktree, lockNote string
	var lockDuration time.Duration
	lock := &cobra.Command{
		Use:   "lock",
		Short: "Create and claim a minimal task in one step, to hold a working-directory lock for ad hoc work with no pre-existing task",
		Long: "task create requires a title, summary, repository, branch, and resource list before " +
			"task claim can acquire a working-directory lock -- real ceremony for a real task, but too " +
			"much for the single most common shape of work in an interactive multi-agent setup: a human " +
			"directly asking a live agent to fix something, with no Task tracked yet. `task lock` creates " +
			"a minimal, throwaway task (auto-generated ID, --note as its title/summary, current branch " +
			"detected via git if not given) and claims it in the same call, so reaching for a lock is " +
			"never more work than skipping one. Complete or cancel it like any other task when the work " +
			"is done (the printed task ID is what you need).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if lockWorktree == "" {
				return errors.New("--worktree is required")
			}
			// task.claim's scopeAllows check requires every task Resource to
			// be covered by the claiming actor's own Scopes -- it's a
			// permission check, unrelated to the separate Worktree-based
			// conflict check below that's the actual point of this command.
			// Using the raw worktree path as the resource (the first thing
			// tried here) fails that check for any actor without a wildcard
			// scope, since a filesystem path essentially never matches a
			// scope tag like "src" -- confirmed live. Reusing the actor's
			// own first registered scope instead means the check always
			// trivially passes for whoever is actually running this command,
			// which is exactly the property an ad hoc, no-fuss lock needs.
			st, e := c.svc.State()
			if e != nil {
				return fmt.Errorf("read state: %w", e)
			}
			agent, ok := st.Agents[c.actor]
			if !ok || len(agent.Scopes) == 0 {
				return fmt.Errorf("actor %q has no registered scopes to lock a task under", c.actor)
			}
			id := fmt.Sprintf("adhoc-%s-%d", c.actor, time.Now().UnixNano())
			note := strings.TrimSpace(lockNote)
			if note == "" {
				note = "Ad hoc working-directory lock (no pre-existing task)"
			}
			branch := gitBranch(lockWorktree)
			if branch == "" {
				branch = "unknown"
			}
			// A bare scope tag ("src") as the resource, not scope+"/"+id,
			// would make every ad hoc lock from same-scoped actors overlap
			// with every other one via the generic write-lease check
			// (transitions.go's overlap()), even across completely
			// unrelated worktrees -- confirmed live: locking a second,
			// entirely different directory was rejected purely because an
			// earlier lock happened to share the "src" scope, nowhere near
			// the same files. Scoping it under a per-lock, id-unique
			// sub-resource still satisfies scopeAllows's own
			// prefix-with-"/" rule, but two separate ad hoc locks' resource
			// strings can now never accidentally overlap each other -- the
			// worktree check right below is the only thing that still can,
			// which is the one conflict this command actually means to catch.
			if _, e := c.svc.Execute(c.actor, "task.create", id, model.TaskCreated{
				Title: note, Summary: note, Repository: "local", Branch: branch,
				Worktree: lockWorktree, Resources: []string{agent.Scopes[0] + "/adhoc/" + id},
			}); e != nil {
				return fmt.Errorf("create ad hoc task: %w", e)
			}
			lease := time.Now().UTC().Add(lockDuration)
			v, e := c.svc.Execute(c.actor, "task.claim", id, model.TaskClaimed{LeaseUntil: lease, Worktree: lockWorktree})
			if e != nil {
				return fmt.Errorf("claim ad hoc task %s: %w", id, e)
			}
			return c.emitDocument("task.lock", v, cliui.Document{
				Title:  "Worktree locked",
				Status: cliui.StatusSuccess,
				Fields: []cliui.Field{
					{Label: "Task", Value: id},
					{Label: "Actor", Value: c.actor},
					{Label: "Worktree", Value: lockWorktree},
					{Label: "Branch", Value: branch},
					{Label: "Lease until", Value: lease.Format(time.RFC3339)},
				},
				Hint: "Use the printed task ID to complete or cancel this lock when the work is done.",
			})
		},
	}
	lock.Flags().StringVar(&lockWorktree, "worktree", "", "worktree path to lock (required)")
	lock.Flags().StringVar(&lockNote, "note", "", "what you're about to do -- used as the ad hoc task's title/summary")
	lock.Flags().DurationVar(&lockDuration, "duration", 4*time.Hour, "lease duration")
	show := c.entityShow("task", func(st model.State, id string) (any, []cliui.Field, bool) {
		t, ok := st.Tasks[id]
		if !ok {
			return nil, nil, false
		}
		return t, []cliui.Field{
			{Label: "Title", Value: t.Title}, {Label: "Status", Value: t.Status},
			{Label: "Owner", Value: t.Owner}, {Label: "Branch", Value: t.Branch},
			{Label: "Worktree", Value: t.Worktree},
		}, true
	})
	root.AddCommand(create, offer, claim, start, renew, block, review, complete, cancel, handoff, takeover, list, lock, show)
	return root
}
