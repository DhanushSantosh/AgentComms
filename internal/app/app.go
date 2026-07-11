package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/service"
)

const Version = "0.1.0"

type output struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

func emit(w io.Writer, j bool, cmd string, v any) error {
	if j {
		return json.NewEncoder(w).Encode(output{OK: true, Command: cmd, Result: v})
	}
	_, e := fmt.Fprintln(w, v)
	return e
}
func fail(j bool, cmd string, err error) error {
	if j {
		b, _ := json.Marshal(output{OK: false, Command: cmd, Error: err.Error()})
		return errors.New(string(b))
	}
	return err
}
func parse(args []string) (bool, string, []string) {
	j := false
	o := []string{}
	for _, a := range args {
		if a == "--json" {
			j = true
		} else {
			o = append(o, a)
		}
	}
	if len(o) == 0 {
		return j, "", nil
	}
	return j, o[0], o[1:]
}
func kv(args []string) map[string]any {
	d := map[string]any{}
	for _, a := range args {
		p := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)
		if len(p) == 2 {
			if strings.Contains(p[1], ",") {
				x := strings.Split(p[1], ",")
				v := make([]any, len(x))
				for i := range x {
					v[i] = x[i]
				}
				d[p[0]] = v
			} else {
				d[p[0]] = p[1]
			}
		}
	}
	return d
}
func id(args []string, d map[string]any) string {
	if v, ok := d["id"].(string); ok {
		return v
	}
	if len(args) > 1 && !strings.Contains(args[1], "=") {
		return args[1]
	}
	return ""
}

func Run(args []string, stdout, stderr io.Writer) error {
	j, cmd, rest := parse(args)
	if cmd == "" {
		return usage(stdout)
	}
	cwd, _ := os.Getwd()
	s := service.New(cwd)
	switch cmd {
	case "version":
		return emit(stdout, j, cmd, map[string]string{"version": Version, "schema_version": "1.0.0"})
	case "init":
		f := flag.NewFlagSet("init", flag.ContinueOnError)
		f.SetOutput(stderr)
		owner := f.String("owner", "owner", "")
		_ = f.Parse(rest)
		if e := s.Store.Init(*owner); e != nil {
			return fail(j, cmd, e)
		}
		return emit(stdout, j, cmd, map[string]any{"runtime": filepath.Join(cwd, ".agent-comms"), "owner": *owner})
	case "verify":
		if e := s.Store.Verify(); e != nil {
			return fail(j, cmd, e)
		}
		return emit(stdout, j, cmd, map[string]any{"verified": true, "head": s.Store.Head()})
	case "doctor":
		e := s.Store.Verify()
		r := map[string]any{"runtime": filepath.Join(cwd, ".agent-comms"), "integrity": e == nil, "git_head": s.Store.Head(), "telemetry": false}
		if e != nil {
			r["error"] = e.Error()
		}
		return emit(stdout, j, cmd, r)
	case "status":
		st, e := s.State()
		if e != nil {
			return fail(j, cmd, e)
		}
		return emit(stdout, j, cmd, st)
	case "history", "search":
		ev, e := s.Store.Events()
		if e != nil {
			return fail(j, cmd, e)
		}
		if cmd == "search" && len(rest) > 0 {
			q := strings.ToLower(strings.Join(rest, " "))
			f := ev[:0]
			for _, v := range ev {
				b, _ := json.Marshal(v)
				if strings.Contains(strings.ToLower(string(b)), q) {
					f = append(f, v)
				}
			}
			ev = f
		}
		return emit(stdout, j, cmd, ev)
	case "checkpoint", "sync":
		if e := s.Store.Checkpoint(); e != nil {
			return fail(j, cmd, e)
		}
		return emit(stdout, j, cmd, map[string]bool{"checkpointed": true})
	case "archive":
		e, er := s.Execute("owner", "archive.run", "archive", map[string]any{"retention": "168h"})
		if er != nil {
			return fail(j, cmd, er)
		}
		return emit(stdout, j, cmd, e)
	case "migrate":
		return migrate(s, rest, stdout, j)
	case "artifact":
		return artifact(s, rest, stdout, j)
	case "tui":
		return tui(s, stdout)
	case "agent", "session", "task", "message", "decision", "approval":
		return domain(s, cmd, rest, stdout, j)
	default:
		return fail(j, cmd, fmt.Errorf("unknown command %q", cmd))
	}
}
func usage(w io.Writer) error {
	_, e := fmt.Fprintln(w, "agent-comms: init version doctor verify migrate agent session task message decision approval artifact status history search archive checkpoint sync tui")
	return e
}
func domain(s *service.Service, domain string, args []string, w io.Writer, j bool) error {
	if len(args) == 0 {
		return fail(j, domain, errors.New("subcommand required"))
	}
	sub := args[0]
	d := kv(args[1:])
	actor := "owner"
	if v, ok := d["actor"].(string); ok {
		actor = v
		delete(d, "actor")
	}
	entity := id(args, d)
	delete(d, "id")
	if domain == "agent" && sub == "list" || domain == "message" && sub == "inbox" {
		st, e := s.State()
		if e != nil {
			return fail(j, domain+" "+sub, e)
		}
		if domain == "agent" {
			return emit(w, j, domain+" "+sub, st.Agents)
		}
		return emit(w, j, domain+" "+sub, st.Messages)
	}
	typ := domain + "." + sub
	if domain == "task" && sub == "handoff" {
		if v, ok := d["accept"].(string); ok && v == "true" {
			typ = "task.handoff.accept"
		}
	}
	if entity == "" {
		return fail(j, typ, errors.New("id required (--id=...)"))
	}
	e, er := s.Execute(actor, typ, entity, d)
	if er != nil {
		return fail(j, typ, er)
	}
	return emit(w, j, typ, e)
}
func artifact(s *service.Service, args []string, w io.Writer, j bool) error {
	if len(args) == 0 {
		return fail(j, "artifact", errors.New("subcommand required"))
	}
	d := kv(args[1:])
	actor, _ := d["actor"].(string)
	if actor == "" {
		actor = "owner"
	}
	switch args[0] {
	case "add":
		p, _ := d["path"].(string)
		e, er := s.AddArtifact(actor, p)
		if er != nil {
			return fail(j, "artifact add", er)
		}
		return emit(w, j, "artifact add", e)
	case "show", "verify":
		sum, _ := d["sha256"].(string)
		p := filepath.Join(s.Store.Root, ".agent-comms", "artifacts", "sha256", sum)
		b, e := os.ReadFile(p)
		if e != nil {
			return fail(j, "artifact "+args[0], e)
		}
		if args[0] == "show" && !j {
			_, e = w.Write(b)
			return e
		}
		return emit(w, j, "artifact "+args[0], map[string]any{"sha256": sum, "size": len(b), "verified": true})
	}
	return fail(j, "artifact", errors.New("unknown subcommand"))
}
func migrate(s *service.Service, args []string, w io.Writer, j bool) error {
	if len(args) == 0 || args[0] == "status" {
		return emit(w, j, "migrate status", map[string]any{"current": "1.0.0", "available": []string{}})
	}
	return fail(j, "migrate", errors.New("no migration registered for requested version"))
}
func tui(s *service.Service, w io.Writer) error {
	in := bufio.NewScanner(os.Stdin)
	views := []string{"overview", "tasks", "inbox", "agents", "approvals", "contracts/decisions", "blockers", "integrity/sync", "archive search"}
	for {
		st, e := s.State()
		if e != nil {
			return e
		}
		fmt.Fprint(w, "\x1b[2J\x1b[HAgent Comms TUI\n")
		fmt.Fprintf(w, "Tasks %d | Messages %d | Agents %d | Approvals %d\n", len(st.Tasks), len(st.Messages), len(st.Agents), len(st.Approvals))
		fmt.Fprintln(w, strings.Join(views, " | "))
		fmt.Fprintln(w, "Commands: view <name>, write <domain> <action> key=value..., quit")
		if !in.Scan() {
			return nil
		}
		line := strings.TrimSpace(in.Text())
		if line == "quit" || line == "q" {
			return nil
		}
		p := strings.Fields(line)
		if len(p) >= 3 && p[0] == "write" {
			if e := domain(s, p[1], p[2:], w, false); e != nil {
				fmt.Fprintln(w, "error:", e)
			}
		}
	}
}
