package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/spf13/cobra"
)

func (c *cli) artifactCmd() *cobra.Command {
	root := &cobra.Command{Use: "artifact", Short: "Store, inspect, and verify content-addressed artifacts"}
	var path, hash string
	add := &cobra.Command{Use: "add", Short: "Store a file as a content-addressed artifact", RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.AddArtifact(c.actor, path)
		if e != nil {
			return e
		}
		return c.emit("artifact.add", v)
	}}
	add.Flags().StringVar(&path, "path", "", "artifact path")
	_ = add.MarkFlagRequired("path")
	show := &cobra.Command{Use: "show", Short: "Show an artifact by its SHA-256", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		a, ok := st.Artifacts[hash]
		if !ok {
			return errors.New("artifact not found")
		}
		return c.emitDocument("artifact.show", a, cliui.Document{
			Title:  "Artifact " + a.Name,
			Status: cliui.StatusInfo,
			Fields: []cliui.Field{
				{Label: "SHA-256", Value: a.SHA256},
				{Label: "Size", Value: fmt.Sprintf("%d bytes", a.Size)},
				{Label: "Media type", Value: a.MediaType},
				{Label: "Storage", Value: a.Storage},
			},
		})
	}}
	show.Flags().StringVar(&hash, "sha256", "", "artifact digest")
	verify := &cobra.Command{Use: "verify", Short: "Verify an artifact's content matches its SHA-256", RunE: func(cmd *cobra.Command, args []string) error {
		p := filepath.Join(c.svc.Store.Root, store.Runtime, "artifacts", "sha256", hash)
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != hash {
			return errors.New("artifact hash mismatch")
		}
		result := map[string]any{"verified": true, "sha256": hash, "size": len(b)}
		if c.json {
			return c.emit("artifact.verify", result)
		}
		return c.emitDocument("artifact.verify", result, cliui.Document{
			Title:  "Artifact verified",
			Status: cliui.StatusSuccess,
			Fields: []cliui.Field{
				{Label: "SHA-256", Value: hash},
				{Label: "Size", Value: fmt.Sprintf("%d bytes", len(b))},
			},
			Hint: "Use artifact show with this digest to inspect its governed metadata.",
		})
	}}
	verify.Flags().StringVar(&hash, "sha256", "", "artifact digest")
	root.AddCommand(add, show, verify)
	return root
}
func (c *cli) documentCmd() *cobra.Command {
	root := &cobra.Command{Use: "document", Short: "Create and manage governed project documents"}
	var title, body, docReplacement, bodyFile string
	var tags []string
	create := &cobra.Command{Use: "create", Short: "Create a governed document", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		if body == "" {
			return errors.New("body is required (use --body or --body-file)")
		}
		v, e := c.svc.Execute(c.actor, "document.create", id, model.DocumentPayload{Title: title, Body: body, Tags: tags})
		if e != nil {
			return e
		}
		return c.emit("document.create", v)
	}}
	create.Flags().String("id", "", "document ID")
	_ = create.MarkFlagRequired("id")
	create.Flags().StringVar(&title, "title", "", "title")
	_ = create.MarkFlagRequired("title")
	create.Flags().StringVar(&body, "body", "", "body")
	create.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	create.Flags().StringSliceVar(&tags, "tag", nil, "tag (repeatable)")
	update := &cobra.Command{Use: "update", Short: "Update a governed document's body or tags", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if bodyFile != "" {
			b, e := os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
			body = string(b)
		}
		if body == "" {
			return errors.New("body is required (use --body or --body-file)")
		}
		v, e := c.svc.Execute(c.actor, "document.update", id, model.DocumentPayload{Title: title, Body: body, Tags: tags})
		if e != nil {
			return e
		}
		return c.emit("document.update", v)
	}}
	update.Flags().String("id", "", "document ID")
	_ = update.MarkFlagRequired("id")
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	update.Flags().StringVar(&bodyFile, "body-file", "", "read body from file (bypasses CLI arg limits)")
	update.Flags().StringSliceVar(&tags, "tag", nil, "tag (repeatable)")
	supersede := &cobra.Command{Use: "supersede", Short: "Replace a document with a newer one", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		docReplacement, _ = cmd.Flags().GetString("replacement")
		v, e := c.svc.Execute(c.actor, "document.supersede", id, model.DocumentPayload{ReplacementID: docReplacement})
		if e != nil {
			return e
		}
		return c.emit("document.supersede", v)
	}}
	supersede.Flags().String("id", "", "document ID")
	_ = supersede.MarkFlagRequired("id")
	supersede.Flags().StringVar(&docReplacement, "replacement", "", "replacement document ID")
	_ = supersede.MarkFlagRequired("replacement")
	list := &cobra.Command{Use: "list", Short: "List governed documents", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		ids := make([]string, 0, len(st.Documents))
		for id := range st.Documents {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			document := st.Documents[id]
			rows = append(rows, []string{id, document.Title, document.Status, fmt.Sprint(document.Version), document.Author})
		}
		return c.emitTable("document.list", st.Documents, []string{"ID", "TITLE", "STATUS", "VERSION", "AUTHOR"}, rows)
	}}
	show := &cobra.Command{Use: "show", Short: "Show one governed document by ID", RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" && len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			return errors.New("document ID required (use --id or positional argument)")
		}
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		d, ok := st.Documents[id]
		if !ok {
			return fmt.Errorf("document %q not found", id)
		}
		return c.emitDocument("document.show", d, cliui.Document{
			Title:  d.Title,
			Status: cliui.StatusInfo,
			Fields: []cliui.Field{
				{Label: "ID", Value: d.ID},
				{Label: "Status", Value: d.Status},
				{Label: "Version", Value: fmt.Sprint(d.Version)},
				{Label: "Author", Value: d.Author},
				{Label: "Tags", Value: strings.Join(d.Tags, ", ")},
				{Label: "Body", Value: d.Body},
			},
		})
	}}
	show.Flags().String("id", "", "document ID")
	root.AddCommand(create, update, supersede, list, show)
	return root
}
func (c *cli) envCmd() *cobra.Command {
	root := &cobra.Command{Use: "env", Short: "Manage governed project environment values"}
	var key, value string
	set := &cobra.Command{Use: "set", Short: "Set a governed environment value", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if value == "" && len(args) > 1 {
			value = args[1]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		v, e := c.svc.Execute(c.actor, "env.set", "", model.EnvSetPayload{Key: key, Value: value})
		if e != nil {
			return e
		}
		return c.emit("env.set", v)
	}}
	set.Flags().StringVar(&key, "key", "", "key")
	set.Flags().StringVar(&value, "value", "", "value")
	get := &cobra.Command{Use: "get", Short: "Get a governed environment value", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		entry, ok := st.Env[key]
		if !ok {
			return fmt.Errorf("key %q not found", key)
		}
		return c.emitDocument("env.get", entry, cliui.Document{
			Title:  "Environment value",
			Status: cliui.StatusInfo,
			Fields: []cliui.Field{
				{Label: "Key", Value: entry.Key},
				{Label: "Value", Value: entry.Value},
				{Label: "Updated by", Value: entry.UpdatedBy},
				{Label: "Updated at", Value: entry.UpdatedAt.Format(time.RFC3339)},
			},
		})
	}}
	get.Flags().StringVar(&key, "key", "", "key")
	del := &cobra.Command{Use: "delete", Short: "Delete a governed environment value", RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" && len(args) > 0 {
			key = args[0]
		}
		if key == "" {
			return errors.New("key required (use --key or positional argument)")
		}
		v, e := c.svc.Execute(c.actor, "env.delete", "", model.EnvDeletePayload{Key: key})
		if e != nil {
			return e
		}
		return c.emit("env.delete", v)
	}}
	del.Flags().StringVar(&key, "key", "", "key")
	list := &cobra.Command{Use: "list", Short: "List governed environment values", RunE: func(cmd *cobra.Command, args []string) error {
		st, e := c.svc.State()
		if e != nil {
			return e
		}
		keys := make([]string, 0, len(st.Env))
		for key := range st.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		rows := make([][]string, 0, len(keys))
		for _, key := range keys {
			entry := st.Env[key]
			rows = append(rows, []string{key, entry.UpdatedBy, entry.UpdatedAt.Format(time.RFC3339)})
		}
		return c.emitTable("env.list", st.Env, []string{"KEY", "UPDATED BY", "UPDATED AT"}, rows)
	}}
	root.AddCommand(set, get, del, list)
	return root
}

func (c *cli) draftCmd() *cobra.Command {
	root := &cobra.Command{Use: "draft", Short: "Manage non-authoritative local drafts"}
	var id, kind, body, bodyFile string
	save := &cobra.Command{Use: "save", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if id == "" || kind == "" {
			return errors.New("--id and --kind are required")
		}
		if body != "" && bodyFile != "" {
			return errors.New("use only one of --body or --body-file")
		}
		var raw []byte
		var e error
		if bodyFile != "" {
			raw, e = os.ReadFile(bodyFile)
			if e != nil {
				return e
			}
		} else if body != "" {
			raw = []byte(body)
		} else {
			return errors.New("--body or --body-file is required")
		}
		if !json.Valid(raw) {
			raw, e = json.Marshal(map[string]string{"content": string(raw)})
			if e != nil {
				return e
			}
		}
		if e = c.svc.SaveDraft(id, strings.ToLower(kind), json.RawMessage(raw)); e != nil {
			return e
		}
		result := map[string]any{"id": id, "kind": strings.ToLower(kind), "authoritative": false}
		return c.emitDocument("draft.save", result, cliui.Document{
			Title: "Local draft saved", Status: cliui.StatusSuccess,
			Fields: []cliui.Field{{Label: "Draft", Value: id}, {Label: "Kind", Value: strings.ToLower(kind)}, {Label: "Authoritative", Value: "no"}},
			Hint:   "Create the corresponding governed object when the draft is ready to become authoritative.",
		})
	}}
	save.Flags().StringVar(&id, "id", "", "draft ID")
	save.Flags().StringVar(&kind, "kind", "", "document, message, or artifact")
	save.Flags().StringVar(&body, "body", "", "draft JSON or text")
	save.Flags().StringVar(&bodyFile, "body-file", "", "read draft content from a file")
	var limit int
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		drafts, e := c.svc.Drafts(limit)
		if e != nil {
			return e
		}
		result := map[string]any{"drafts": drafts, "authoritative": false}
		rows := make([][]string, 0, len(drafts))
		for _, draft := range drafts {
			rows = append(rows, []string{draft.ID, draft.Kind, draft.UpdatedAt.Format(time.RFC3339), "local"})
		}
		return c.emitTable("draft.list", result, []string{"ID", "KIND", "UPDATED", "AUTHORITY"}, rows)
	}}
	list.Flags().IntVar(&limit, "limit", controlplane.DefaultPageSize, "maximum drafts to return")
	save.Short = "Save a non-authoritative local draft"
	list.Short = "List local drafts"
	var showID string
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, Short: "Show one local draft by ID", RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(showID) == "" {
			return errors.New("--id is required")
		}
		drafts, e := c.svc.Drafts(0)
		if e != nil {
			return e
		}
		for _, draft := range drafts {
			if draft.ID == showID {
				return c.emitDocument("draft.show", draft, cliui.Document{
					Title: "Local draft " + draft.ID, Status: cliui.StatusInfo,
					Fields: []cliui.Field{
						{Label: "Kind", Value: draft.Kind}, {Label: "Updated", Value: draft.UpdatedAt.Format(time.RFC3339)},
						{Label: "Body", Value: string(draft.Body)},
					},
				})
			}
		}
		return fmt.Errorf("draft %q not found", showID)
	}}
	show.Flags().StringVar(&showID, "id", "", "draft ID")
	root.AddCommand(save, list, show)
	return root
}

func (c *cli) archiveCmd() *cobra.Command {
	return &cobra.Command{Use: "archive", RunE: func(cmd *cobra.Command, args []string) error {
		v, e := c.svc.Archive(c.actor)
		if e != nil {
			return e
		}
		return c.emit("archive", v)
	}}
}
func (c *cli) exportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{Use: "export <jsonl|markdown>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var w io.Writer = c.out
		var f *os.File
		var e error
		if out != "" {
			f, e = os.Create(out)
			if e != nil {
				return e
			}
			defer f.Close()
			w = f
		}
		switch args[0] {
		case "jsonl":
			e = c.svc.ExportJSONL(w)
		case "markdown":
			e = c.svc.ExportMarkdown(w)
		default:
			return errors.New("format must be jsonl or markdown")
		}
		return e
	}}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output file")
	return cmd
}
