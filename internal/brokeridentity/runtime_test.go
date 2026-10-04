package brokeridentity

import (
	"strings"
	"testing"
)

func TestRuntimeKey(t *testing.T) {
	seen := map[string]bool{}
	for _, pair := range [][2]string{{"ab", "c"}, {"a", "bc"}, {"project-a", "runtime"}, {"project-b", "runtime"}, {"项目", "runtime"}} {
		key, err := RuntimeKey(pair[0], pair[1])
		if err != nil || !ValidRuntimeID(key) || len(key) != 66 {
			t.Fatalf("invalid key %q: %v", key, err)
		}
		if seen[key] {
			t.Fatal("distinct pairs collided")
		}
		seen[key] = true
		again, _ := RuntimeKey(pair[0], pair[1])
		if again != key {
			t.Fatal("key is unstable")
		}
	}
	for _, pair := range [][2]string{{"", "runtime"}, {" ", "runtime"}, {"project", ""}, {"project", "../bad"}, {"project", strings.Repeat("x", 129)}} {
		if _, err := RuntimeKey(pair[0], pair[1]); err == nil {
			t.Fatalf("accepted invalid pair: %q", pair)
		}
	}
}
