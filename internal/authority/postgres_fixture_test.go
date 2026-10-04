package authority

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Schema-mutating tests must not change the shared database used by daemon
// integration tests running in another package. Each owns a fresh database;
// cleanup names only that generated fixture, never the configured base.
func migrationDatabaseURL(t *testing.T) string {
	t.Helper()
	baseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		t.Fatal("migration fixtures require a PostgreSQL connection URL")
	}
	admin, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	name := "agc_migration_" + uuid.NewString()
	identifier := pgx.Identifier{name}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0"); err != nil {
		_ = admin.Close()
		t.Fatalf("create isolated migration fixture (test role requires CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := admin.ExecContext(cleanupContext, "DROP DATABASE "+identifier); cleanupErr != nil {
			t.Errorf("drop owned migration fixture %s: %v", name, cleanupErr)
		}
	})
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String()
}

func TestMigrationFixtureDoesNotContaminateSharedDatabase(t *testing.T) {
	sharedURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if sharedURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	shared, err := sql.Open("pgx", sharedURL)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	if err = ApplySchema(ctx, shared, false); err != nil {
		t.Fatal(err)
	}
	fixture, err := sql.Open("pgx", migrationDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	if err = ApplySchema(ctx, fixture, false); err != nil {
		t.Fatal(err)
	}
	const future = 900003
	if _, err = fixture.ExecContext(ctx, `INSERT INTO schema_migrations (version,name,checksum,build_id) VALUES ($1,'fixture-isolation','x','test')`, future); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, cleanupErr := fixture.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=$1`, future); cleanupErr != nil {
			t.Errorf("remove owned fixture marker: %v", cleanupErr)
		}
	}()
	if err = ApplySchema(ctx, fixture, false); err == nil {
		t.Fatal("future-version fixture must still exercise refusal")
	}
	if err = ApplySchema(ctx, shared, false); err != nil {
		t.Fatalf("migration fixture contaminated concurrently used shared database: %v", err)
	}
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	observer, err := Open(ctx, Config{DatabaseURL: sharedURL}, signer)
	if err != nil {
		t.Fatalf("unrelated authority startup must succeed while fixture has a future schema: %v", err)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
}
