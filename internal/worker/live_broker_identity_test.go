package worker

import "testing"

func TestLiveBrokerProjectIdentity(t *testing.T) {
	instance, _ := workerService(t)
	cfg, err := instance.Store.ConfigStrict()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{t.TempDir(), t.TempDir()} {
		id, err := liveBrokerProjectID(Config{Service: instance, RuntimeID: "runtime-axiom", WorkDir: root})
		if err != nil || id != cfg.ProjectID {
			t.Fatalf("identity must survive relocation: %q %v", id, err)
		}
	}
	if _, err := liveBrokerProjectID(Config{RuntimeID: "runtime"}); err == nil {
		t.Fatal("missing project service accepted")
	}
	if _, err := liveBrokerProjectID(Config{Service: instance, RuntimeID: "../bad"}); err == nil {
		t.Fatal("invalid runtime accepted")
	}
}
