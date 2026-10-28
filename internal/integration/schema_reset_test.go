package integration

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetTestSchemaRecreatesMissingPublicSchema(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	db := openSchemaResetTestDB(t, databaseURL)
	_, err := db.ExecContext(t.Context(), "DROP SCHEMA IF EXISTS public CASCADE")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, cleanupErr := db.ExecContext(cleanupCtx, "CREATE SCHEMA IF NOT EXISTS public")
		assert.NoError(t, cleanupErr)
	})

	err = resetTestSchemaContext(t.Context(), databaseURL)
	require.NoError(t, err)

	var exists bool
	err = db.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'public')").Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestResetTestSchemaRollsBackWhenCreatingPublicFails(t *testing.T) {
	requireSchemaResetEventTriggerPermission(t)
	databaseURL := testDatabaseURL(t)
	db := openSchemaResetTestDB(t, databaseURL)
	_, err := db.ExecContext(t.Context(), "CREATE SCHEMA IF NOT EXISTS public")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "DROP TABLE IF EXISTS public.reset_schema_sentinel")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "CREATE TABLE public.reset_schema_sentinel (value text NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO public.reset_schema_sentinel (value) VALUES ('preserve me')")
	require.NoError(t, err)
	installSchemaCreateTrigger(t, `RAISE EXCEPTION 'intentional schema creation failure';`)

	err = resetTestSchemaContext(t.Context(), databaseURL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intentional schema creation failure")
	assertSchemaSentinel(t, db)
}

func TestResetTestSchemaAllowsCreateSlowerThanFormerTimeout(t *testing.T) {
	requireSchemaResetEventTriggerPermission(t)
	databaseURL := testDatabaseURL(t)
	installSchemaCreateTrigger(t, `PERFORM pg_sleep(11);`)

	started := time.Now()
	resetTestSchema(t, databaseURL)
	assert.GreaterOrEqual(t, time.Since(started), 10*time.Second)
}

func TestResetTestSchemaCancellationRollsBack(t *testing.T) {
	requireSchemaResetEventTriggerPermission(t)
	databaseURL := testDatabaseURL(t)
	db := openSchemaResetTestDB(t, databaseURL)
	_, err := db.ExecContext(t.Context(), "CREATE SCHEMA IF NOT EXISTS public")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "DROP TABLE IF EXISTS public.reset_schema_sentinel")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "CREATE TABLE public.reset_schema_sentinel (value text NOT NULL)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO public.reset_schema_sentinel (value) VALUES ('preserve me')")
	require.NoError(t, err)
	installSchemaCreateTrigger(t, `PERFORM pg_sleep(3);`)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	err = resetTestSchemaContext(ctx, databaseURL)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assertSchemaSentinel(t, db)
}

func TestResetTestSchemaRefusesDifferentDatabase(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	otherDatabaseURL := schemaResetURLForDatabase(t, databaseURL, "postgres")
	db, err := sql.Open("pgx", otherDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	if err := db.PingContext(t.Context()); err != nil {
		t.Skip("test role cannot connect to postgres database")
	}

	err = resetTestSchemaContext(t.Context(), otherDatabaseURL)
	require.Error(t, err)
	assert.ErrorContains(t, err, `refusing to reset schema in database "postgres"`)
	assert.NotContains(t, err.Error(), otherDatabaseURL)
}

func installSchemaCreateTrigger(t *testing.T, action string) {
	t.Helper()
	db := openSchemaResetTestDB(t, testDatabaseURL(t))
	_, err := db.ExecContext(t.Context(), "DROP EVENT TRIGGER IF EXISTS vane_reset_test_event_trigger")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "DROP SCHEMA IF EXISTS vane_reset_test_harness CASCADE")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "CREATE SCHEMA vane_reset_test_harness")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, cleanupErr := db.ExecContext(cleanupCtx, "DROP EVENT TRIGGER IF EXISTS vane_reset_test_event_trigger")
		assert.NoError(t, cleanupErr)
		_, cleanupErr = db.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS vane_reset_test_harness CASCADE")
		assert.NoError(t, cleanupErr)
	})
	_, err = db.ExecContext(t.Context(), `CREATE FUNCTION vane_reset_test_harness.before_schema_create() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN `+action+` END; $$`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "CREATE EVENT TRIGGER vane_reset_test_event_trigger ON ddl_command_start WHEN TAG IN ('CREATE SCHEMA') EXECUTE FUNCTION vane_reset_test_harness.before_schema_create()")
	require.NoError(t, err)
}

func requireSchemaResetEventTriggerPermission(t *testing.T) {
	t.Helper()
	db := openSchemaResetTestDB(t, testDatabaseURL(t))
	var isSuperuser bool
	err := db.QueryRowContext(t.Context(), "SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&isSuperuser)
	require.NoError(t, err)
	if !isSuperuser {
		t.Skip("event-trigger failure injection requires PostgreSQL superuser privileges")
	}
}

func assertSchemaSentinel(t *testing.T, db *sql.DB) {
	t.Helper()
	var value string
	err := db.QueryRowContext(t.Context(), "SELECT value FROM public.reset_schema_sentinel").Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "preserve me", value)
}

func openSchemaResetTestDB(t *testing.T, databaseURL string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	var database string
	err = db.QueryRowContext(t.Context(), "SELECT current_database()").Scan(&database)
	require.NoError(t, err)
	require.Equal(t, "vane_test", database, "refusing destructive fixture setup outside vane_test")
	return db
}

func schemaResetURLForDatabase(t *testing.T, databaseURL, database string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	parsed.Path = "/" + database
	return parsed.String()
}
