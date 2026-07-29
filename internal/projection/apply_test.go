package projection

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestLegacyInvocationAndRuntimeProjectToCompatibilityDefaults(t *testing.T) {
	state := model.State{}
	requestData, err := json.Marshal(model.InvocationRequested{
		Target: "builder", Instruction: "Review the project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-request", Time: time.Now().UTC(), Actor: "owner",
		Type: "invocation.request", EntityID: "invocation", Data: requestData,
	}); err != nil {
		t.Fatal(err)
	}
	runtimeData, err := json.Marshal(model.RuntimeRegistered{
		AgentID: "builder", Connector: "MCP", MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-runtime", Time: time.Now().UTC(), Actor: "builder",
		Type: "runtime.register", EntityID: "runtime", Data: runtimeData,
	}); err != nil {
		t.Fatal(err)
	}
	if state.Invocations["invocation"].ConsumerMode != model.ConsumerModeEither {
		t.Fatalf("legacy invocation consumer mode=%q, want EITHER",
			state.Invocations["invocation"].ConsumerMode)
	}
	if state.AgentRuntimes["runtime"].Kind != model.RuntimeKindWorker {
		t.Fatalf("legacy runtime kind=%q, want WORKER", state.AgentRuntimes["runtime"].Kind)
	}
}
