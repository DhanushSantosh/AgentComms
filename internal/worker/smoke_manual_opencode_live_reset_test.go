package worker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

func TestManualSmokeOpenCodeLiveResetCompletion(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_LIVE_WORKER_POLICY_SMOKE=1 for actual native reset completion verification")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	server, err := startOpenCodeOwnedServer(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	}()
	client := opencodeclient.New(server.baseURL, root)
	for turn := 0; turn < 3; turn++ {
		if err := client.DisposeInstance(ctx); err != nil {
			t.Fatalf("real native reset %d completion: %v", turn, err)
		}
		t.Logf("native reset %d acknowledged and exact project disposal completion observed", turn)
	}
}
