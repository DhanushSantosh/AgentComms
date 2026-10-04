package opencodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRestrictivePermissionsPreservesDenyAndOrder(t *testing.T) {
	agent := []PermissionRule{{"*", "*", "allow"}, {"edit", "private/*", "deny"}, {"read", "*.env", "ask"}}
	original := []PermissionRule{{"edit", "docs/*", "allow"}, {"edit", "docs/secret/*", "deny"}}
	want := []PermissionRule{{"*", "*", "ask"}, {"*", "*", "ask"}, {"edit", "private/*", "deny"}, {"read", "*.env", "ask"}, {"edit", "docs/*", "ask"}, {"edit", "docs/secret/*", "deny"}}
	got, err := RestrictivePermissions(agent, original)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("rules=%v error=%v", got, err)
	}
	if agent[0].Action != "allow" || original[0].Action != "allow" {
		t.Fatal("input restrictions were mutated")
	}
	for _, bad := range []PermissionRule{{"edit", "*", "always"}, {"", "*", "deny"}, {"edit", "", "allow"}} {
		if _, err := RestrictivePermissions(agent, []PermissionRule{bad}); err == nil {
			t.Fatalf("unsafe rule accepted: %v", bad)
		}
	}
}

func TestResolveAgentRequiresExactIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, configured, agents string
		fail                     bool
	}{
		{"native-build", "build", `[{"name":"build","mode":"primary","native":true,"permission":[]}]`, false},
		{"explicit-custom", "reviewer", `[{"name":"reviewer","mode":"all","permission":[{"permission":"edit","pattern":"*","action":"deny"}]}]`, false},
		{"no-native-default", "", `[{"name":"other","mode":"primary","permission":[]}]`, true},
		{"empty-identity", "", `[{"name":"build","mode":"primary","permission":[]}]`, true},
		{"missing-rules", "build", `[{"name":"build","mode":"primary"}]`, true},
		{"hidden", "build", `[{"name":"build","mode":"primary","hidden":true,"permission":[]}]`, true},
		{"subagent", "build", `[{"name":"build","mode":"subagent","permission":[]}]`, true},
		{"duplicate", "build", `[{"name":"build","mode":"primary","permission":[]},{"name":"build","mode":"primary","permission":[]}]`, true},
		{"unknown-action", "build", `[{"name":"build","mode":"primary","permission":[{"permission":"*","pattern":"*","action":"new-action"}]}]`, true},
		{"lookup-error", "build", ``, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/config":
					t.Error("resolved configuration must not be fetched")
					http.NotFound(w, r)
				case "/agent":
					if tc.name == "lookup-error" {
						w.WriteHeader(500)
						w.Write([]byte("SYNTHETIC_PRIVATE_MARKER"))
						return
					}
					w.Write([]byte(tc.agents))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			agent, err := New(server.URL, "").ResolveAgent(context.Background(), tc.configured)
			if (err != nil) != tc.fail {
				t.Fatalf("agent=%v error=%v", agent, err)
			}
			if err != nil && strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_MARKER") {
				t.Fatal("private config leaked through error")
			}
		})
	}
}
