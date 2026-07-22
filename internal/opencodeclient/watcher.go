package opencodeclient

import (
	"context"
	"sync"
)

// EditGate reports whether "edit"-category permission requests may be
// auto-approved for the current invocation — the same mode-gated concept
// acpclient.EditGate expresses for ACP-driven adapters.
type EditGate func() bool

// GovernanceApprover resolves a permission request Classify has routed to
// governance rather than deciding locally. Mirrors acpclient.GovernanceApprover.
type GovernanceApprover interface {
	Approve(ctx context.Context, sessionID, permission string, patterns []string) (bool, error)
}

// PermissionWatcher applies the hybrid approval policy (Classify +
// EditGate + GovernanceApprover) to every "permission.asked" event it sees
// on a server's SSE stream, replying through the same Client. It must be
// running (via Run, in its own goroutine) before a blocking Prompt call
// that might raise a mid-turn permission request, or that request would
// never get answered and Prompt would hang until the turn's own timeout.
type PermissionWatcher struct {
	client     *Client
	allowEdits EditGate
	governance GovernanceApprover

	mu          sync.Mutex
	deniedKinds []string
}

// NewPermissionWatcher constructs a watcher bound to client, deciding
// mode-gated requests via allowEdits and governed requests via governance.
func NewPermissionWatcher(client *Client, allowEdits EditGate, governance GovernanceApprover) *PermissionWatcher {
	return &PermissionWatcher{client: client, allowEdits: allowEdits, governance: governance}
}

// Run processes events until the channel closes or ctx is cancelled,
// replying to every "permission.asked" event it observes. Intended to run
// in its own goroutine for the duration of one Prompt call.
func (w *PermissionWatcher) Run(ctx context.Context, events <-chan Event) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Type != "permission.asked" {
				continue
			}
			w.handle(ctx, event)
		case <-ctx.Done():
			return
		}
	}
}

func (w *PermissionWatcher) handle(ctx context.Context, event Event) {
	var request PermissionRequest
	if err := decodeInto(event.Properties, &request); err != nil || request.ID == "" {
		return
	}
	approved, err := w.decide(ctx, request)
	if err != nil {
		approved = false
	}
	reply := "reject"
	if approved {
		reply = "once"
	} else {
		w.recordDenied(request.Permission)
	}
	_ = w.client.ReplyPermission(ctx, request.ID, reply, "")
}

func (w *PermissionWatcher) decide(ctx context.Context, request PermissionRequest) (bool, error) {
	switch Classify(request.Permission) {
	case DecisionAutoApprove:
		return true, nil
	case DecisionModeGated:
		return w.allowEdits(), nil
	default:
		return w.governance.Approve(ctx, request.SessionID, request.Permission, request.Patterns)
	}
}

func (w *PermissionWatcher) recordDenied(permission string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deniedKinds = append(w.deniedKinds, permission)
}

// Denied reports whether any permission request was refused since the last
// ResetTurn. Combined with empty prompt output, this indicates the agent
// gave up silently rather than explaining it couldn't proceed — the same
// signal acpclient.Session.Denied provides for ACP-driven adapters.
func (w *PermissionWatcher) Denied() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.deniedKinds) > 0
}

// DeniedKinds returns the permission category of every request denied since
// the last ResetTurn, in order.
func (w *PermissionWatcher) DeniedKinds() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.deniedKinds...)
}

// ResetTurn clears denial tracking before starting a new prompt turn.
func (w *PermissionWatcher) ResetTurn() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deniedKinds = nil
}
