package app

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestVersionJSONDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	if e := Run([]string{"version", "--json"}, &a, &b); e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e := json.Unmarshal(a.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if v["ok"] != true || v["command"] != "version" {
		t.Fatal(v)
	}
}
func TestTUIViewsDeclared(t *testing.T) {
	old := os.Stdin
	f, e := os.CreateTemp(t.TempDir(), "in")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString("q\n")
	_, _ = f.Seek(0, 0)
	os.Stdin = f
	defer func() { os.Stdin = old }()
	defer f.Close()
	d := t.TempDir()
	wd, _ := os.Getwd()
	_ = os.Chdir(d)
	defer os.Chdir(wd)
	var out bytes.Buffer
	if e := Run([]string{"init"}, &out, &out); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := Run([]string{"tui"}, &out, &out); e != nil {
		t.Fatal(e)
	}
	for _, x := range []string{"overview", "tasks", "inbox", "agents", "approvals", "contracts/decisions", "blockers", "integrity/sync", "archive search"} {
		if !bytes.Contains(out.Bytes(), []byte(x)) {
			t.Errorf("missing %s", x)
		}
	}
}
