package service_test

import (
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestDocumentLifecycleAndValidation(t *testing.T) {
	s := setup(t)
	must(t, s, "owner", "document.create", "guide-v1", model.DocumentPayload{Title: "Guide", Body: "First version", Tags: []string{"reference"}})
	must(t, s, "owner", "document.update", "guide-v1", model.DocumentPayload{Title: "Guide", Body: "Second version", Tags: []string{"reference", "current"}})
	must(t, s, "owner", "document.create", "guide-v2", model.DocumentPayload{Title: "Replacement", Body: "Replacement body"})
	must(t, s, "owner", "document.supersede", "guide-v1", model.DocumentPayload{ReplacementID: "guide-v2"})

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Documents["guide-v1"].Status != "SUPERSEDED" || st.Documents["guide-v1"].Version != 2 {
		t.Fatalf("unexpected old document: %#v", st.Documents["guide-v1"])
	}
	if st.Documents["guide-v2"].Status != "ACTIVE" || st.Documents["guide-v2"].Supersedes != "guide-v1" {
		t.Fatalf("unexpected replacement: %#v", st.Documents["guide-v2"])
	}

	for name, tc := range map[string]struct {
		typ, id string
		payload model.DocumentPayload
	}{
		"empty update":        {"document.update", "guide-v2", model.DocumentPayload{}},
		"missing replacement": {"document.supersede", "guide-v2", model.DocumentPayload{ReplacementID: "missing"}},
		"self replacement":    {"document.supersede", "guide-v2", model.DocumentPayload{ReplacementID: "guide-v2"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := s.Execute("owner", tc.typ, tc.id, tc.payload); e == nil {
				t.Fatal("invalid document transition succeeded")
			}
		})
	}
}
