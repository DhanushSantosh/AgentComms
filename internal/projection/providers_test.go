package projection

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func applyProviderEvent(t *testing.T, state *model.State, sequence uint64, typ, actor string, at time.Time, payload any) {
	t.Helper()
	data, err := model.EncodePayload(typ, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(state, model.Event{ID: typ + "-" + at.String(), Sequence: sequence, Type: typ, EntityID: "gemini", Actor: actor, Time: at, Data: data}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRegisterRetireReactivateReplay(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	state := model.EmptyState()
	applyProviderEvent(t, &state, 4, "provider.register", "owner", t0, model.ProviderRegistered{DisplayName: "Google Gemini CLI", Description: "trial"})
	got := state.Providers["gemini"]
	if got.Name != "gemini" || got.Status != model.ProviderStatusActive || got.AddedBy != "owner" || got.DisplayName != "Google Gemini CLI" {
		t.Fatalf("registered = %+v", got)
	}
	if !got.CreatedAt.Equal(t0) || got.CreatedSequence != 4 || got.UpdatedSequence != 4 {
		t.Fatalf("clock = %+v, want created and updated at sequence 4", got.EntityClock)
	}

	applyProviderEvent(t, &state, 9, "provider.retire", "claude-orchestra", t0.Add(time.Hour), model.ProviderRetired{Reason: "trial ended"})
	got = state.Providers["gemini"]
	if got.Status != model.ProviderStatusRetired || got.RetiredBy != "claude-orchestra" || got.RetireReason != "trial ended" || got.UpdatedSequence != 9 {
		t.Fatalf("retired = %+v", got)
	}

	applyProviderEvent(t, &state, 12, "provider.register", "lead", t0.Add(2*time.Hour), model.ProviderRegistered{})
	got = state.Providers["gemini"]
	if got.Status != model.ProviderStatusActive || got.RetiredBy != "" || got.RetireReason != "" || got.AddedBy != "lead" {
		t.Fatalf("reactivated = %+v", got)
	}
	if !got.CreatedAt.Equal(t0) || got.CreatedSequence != 4 || got.UpdatedSequence != 12 {
		t.Fatalf("reactivation must keep the original creation clock, got %+v", got.EntityClock)
	}
}

// A snapshot written before RFC 0050 has no providers collection; replaying
// a provider event onto it must not panic on a nil map.
func TestProviderEventOntoAPreProviderSnapshot(t *testing.T) {
	state := model.EmptyState()
	state.Providers = nil
	applyProviderEvent(t, &state, 1, "provider.register", "owner", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), model.ProviderRegistered{})
	if state.Providers["gemini"].Status != model.ProviderStatusActive {
		t.Fatal("provider not projected onto an older snapshot")
	}
}
