package authority

import (
	"strings"
	"testing"
)

// TestSchemaMigrationsAreOrderedAndClassified is a pure, non-DB unit test
// of the migration list's own invariants: ApplySchema iterates
// schemaMigrations in slice order and applies every entry regardless of
// its Automatic flag except when a pending entry is disruptive and the
// caller hasn't passed --allow-disruptive, so a gap in version numbering
// or an out-of-order slice would silently reorder or skip migrations at
// startup with no test ever catching it.
func TestSchemaMigrationsAreOrderedAndClassified(t *testing.T) {
	if len(schemaMigrations) == 0 {
		t.Fatal("expected at least one registered schema migration")
	}
	previous := 0
	for _, migration := range schemaMigrations {
		if migration.Version <= previous {
			t.Fatalf("migration versions must be strictly increasing: %d follows %d", migration.Version, previous)
		}
		previous = migration.Version
		if migration.Name == "" {
			t.Fatalf("migration %d has no name", migration.Version)
		}
		if migration.SQL == "" {
			t.Fatalf("migration %d has no SQL", migration.Version)
		}
	}
	if previous != CurrentSchemaVersion {
		t.Fatalf("CurrentSchemaVersion=%d does not match the highest registered migration version %d", CurrentSchemaVersion, previous)
	}
	if !schemaMigrations[0].Automatic {
		t.Fatal("the initial schema migration must remain automatic -- it only creates tables that don't yet exist")
	}
	if strings.Contains(schemaMigrations[0].SQL, "actor_key_fingerprint") {
		t.Fatal("migration v1 was modified; existing installations depend on its recorded checksum")
	}
	if !strings.Contains(schemaMigrations[1].SQL, "actor_key_fingerprint") {
		t.Fatal("migration v2 does not add the actor key fingerprint column")
	}
}
