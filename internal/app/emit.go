package app

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/cliui"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// CLI output rendering: the emit* helpers turn a command's result into a
// --json envelope, a semantic document, a table, or a timeline, plus the
// mutation-receipt builder. Split out of app.go (RFC-free internal
// refactor).

func (c *cli) emit(command string, v any, warnings ...string) error {
	return c.emitWithDelivery(command, v, nil, warnings...)
}

func (c *cli) emitStream(command, event string, data any) error {
	return json.NewEncoder(c.out).Encode(StreamEnvelope{
		APIVersion: APIVersion, Command: command, Event: event,
		Timestamp: time.Now().UTC(), Data: data,
	})
}

func (c *cli) emitDocument(command string, value any, document cliui.Document, warnings ...string) error {
	if c.json {
		return c.emit(command, value, warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	if c.quiet {
		return c.renderWarnings(mode, warnings)
	}
	if c.verbose {
		document.Fields = append(document.Fields,
			cliui.Field{Label: "Command", Value: c.cmd},
			cliui.Field{Label: "Output", Value: string(mode)},
			cliui.Field{Label: "Project", Value: c.project},
			cliui.Field{Label: "Actor", Value: c.actor},
		)
	}
	presenter := cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}
	if err := presenter.Render(document); err != nil {
		return err
	}
	if c.details {
		if err := presenter.RenderDetails(value); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

func (c *cli) renderWarnings(mode cliui.Mode, warnings []string) error {
	presenter := cliui.Presenter{
		Out:          c.err,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.err, c.noColor),
	}
	for _, warning := range warnings {
		if err := presenter.RenderWarning(warning); err != nil {
			return err
		}
	}
	return nil
}

func (c *cli) progress() *cliui.Progress {
	mode := cliui.Mode(c.output)
	if c.json {
		mode = cliui.ModeJSON
	}
	return cliui.NewProgress(c.err, mode, cliui.DetectCapabilities(c.err, c.noColor), c.quiet)
}

// emitTable is emit's counterpart for list-shaped output. JSON mode retains
// the same envelope and payload as emit. Human and plain modes use the shared,
// display-width-aware table presenter.
func (c *cli) emitTable(command string, v any, headers []string, rows [][]string, warnings ...string) error {
	if c.json || c.quiet {
		return c.emit(command, v, warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	if err := (cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}).RenderTable(cliui.Table{Headers: headers, Rows: rows}); err != nil {
		return err
	}
	presenter := cliui.Presenter{Out: c.out, Mode: mode, Capabilities: cliui.DetectCapabilities(c.out, c.noColor)}
	if c.verbose {
		if err := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "project": c.project, "actor": c.actor}); err != nil {
			return err
		}
	}
	if c.details {
		if err := presenter.RenderDetails(v); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

func (c *cli) emitTimeline(command string, value any, timeline cliui.Timeline, warnings ...string) error {
	if c.json {
		return c.emit(command, value, warnings...)
	}
	if c.quiet {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		mode := cliui.Mode(c.output)
		if mode == "" {
			mode = cliui.ModeHuman
		}
		return c.renderWarnings(mode, warnings)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	presenter := cliui.Presenter{Out: c.out, Mode: mode, Capabilities: cliui.DetectCapabilities(c.out, c.noColor)}
	if err := presenter.RenderTimeline(timeline); err != nil {
		return err
	}
	if c.verbose {
		if err := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "project": c.project, "actor": c.actor}); err != nil {
			return err
		}
	}
	if c.details {
		if err := presenter.RenderDetails(value); err != nil {
			return err
		}
	}
	return c.renderWarnings(mode, warnings)
}

// renderTable writes headers and rows as a plain-text table, columns
// padded to the widest cell in each column (header included), separated
// by two spaces. Deliberately no box-drawing characters: those need
// display-width handling for anything beyond plain ASCII to stay aligned,
// and this output is meant to copy-paste cleanly into another command or
// a message, which a bordered table doesn't do as well.
func renderTable(out io.Writer, headers []string, rows [][]string) {
	_ = (cliui.Presenter{Out: out, Mode: cliui.ModePlain}).RenderTable(cliui.Table{Headers: headers, Rows: rows})
}

func (c *cli) emitWithDelivery(command string, v, delivery any, warnings ...string) error {
	if c.json {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		return json.NewEncoder(c.out).Encode(Envelope{
			APIVersion: APIVersion, OK: true, Command: command,
			Result: v, Delivery: delivery, Warnings: warnings,
		})
	}
	if c.quiet {
		if len(c.pendingWarnings) > 0 {
			warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
		}
		mode := cliui.Mode(c.output)
		if mode == "" {
			mode = cliui.ModeHuman
		}
		return c.renderWarnings(mode, warnings)
	}
	if event, ok := v.(model.Event); ok {
		return c.emitDocument(command, v, mutationReceipt(command, event, delivery), warnings...)
	}
	if len(c.pendingWarnings) > 0 {
		warnings = append(append([]string{}, c.pendingWarnings...), warnings...)
	}
	mode := cliui.Mode(c.output)
	if mode == "" {
		mode = cliui.ModeHuman
	}
	presenter := cliui.Presenter{
		Out:          c.out,
		Mode:         mode,
		Capabilities: cliui.DetectCapabilities(c.out, c.noColor),
	}
	if e := presenter.RenderResult(command, v, delivery); e != nil {
		return e
	}
	if c.verbose {
		if e := presenter.RenderSection("Operational context", map[string]any{"command": c.cmd, "output": mode, "project": c.project, "actor": c.actor}); e != nil {
			return e
		}
	}
	return c.renderWarnings(mode, warnings)
}

func mutationReceipt(command string, event model.Event, delivery any) cliui.Document {
	parts := strings.Split(command, ".")
	domain, operation := "Operation", "completed"
	if len(parts) > 0 {
		domain = strings.ToUpper(parts[0][:1]) + parts[0][1:]
	}
	if len(parts) > 1 {
		operation = mutationVerb(parts[len(parts)-1])
	}
	fields := []cliui.Field{
		{Label: "Entity", Value: event.EntityID},
		{Label: "Actor", Value: event.Actor},
		{Label: "Event", Value: event.Type},
		{Label: "Sequence", Value: fmt.Sprint(event.Sequence)},
	}
	if event.Consistency != "" {
		fields = append(fields, cliui.Field{Label: "Consistency", Value: event.Consistency})
	}
	if event.KeyFingerprint != "" {
		fields = append(fields, cliui.Field{Label: "Signing key", Value: event.KeyFingerprint})
	}
	if outcome, ok := delivery.(service.InvocationDeliveryResult); ok {
		fields = append(fields,
			cliui.Field{Label: "Delivery", Value: outcome.Outcome},
			cliui.Field{Label: "Runtime", Value: outcome.RuntimeID},
		)
	}
	return cliui.Document{
		Title: domain + " " + operation, Status: cliui.StatusSuccess, Fields: fields,
		Hint: "Use --details for signed receipt metadata or --json for the stable machine envelope.",
	}
}

func mutationVerb(operation string) string {
	verbs := map[string]string{
		"create": "created", "register": "registered", "activate": "activated",
		"configure": "configured", "update": "updated", "set": "updated",
		"add": "added", "save": "saved", "post": "posted", "request": "requested",
		"offer": "offered", "claim": "claimed", "start": "started", "renew": "renewed",
		"complete": "completed", "resolve": "resolved", "approve": "approved",
		"reject": "rejected", "cancel": "cancelled", "delete": "deleted",
		"revoke": "revoked", "suspend": "suspended", "rename": "renamed",
		"supersede": "superseded", "takeover": "taken over", "handoff": "handed off",
		"heartbeat": "heartbeat recorded", "drain": "draining", "expire": "expired",
		"redeliver": "redelivered", "rotate-key": "key rotated", "elevate-key": "elevated key registered",
	}
	if verb := verbs[operation]; verb != "" {
		return verb
	}
	return strings.ReplaceAll(operation, "-", " ")
}
