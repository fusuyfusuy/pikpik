package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// dbExecutor abstracts *sql.DB and *sql.Tx
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SQLiteStore implements Store on top of SQLite in WAL mode.
type SQLiteStore struct {
	db   *sql.DB
	isTx bool

	orgs                OrganizationStore
	users               UserStore
	sessions            SessionStore
	tokens              APITokenStore
	projects            ProjectStore
	stages              StageStore
	services            ServiceStore
	envVars             EnvVarStore
	volumes             VolumeStore
	stacks              StackStore
	networks            NetworkStore
	deployments         DeploymentStore
	backups             BackupStore
	audit               AuditStore
	builds              BuildStore
	githubInstallations GitHubInstallationStore
	schedules           ScheduleStore
	machines            MachineStore
	notifications       NotificationStore
}

// perConnectionPragmaParams are SQLite session-level settings that database/sql
// does NOT preserve across pooled connections: foreign_keys, busy_timeout,
// synchronous, journal_size_limit, mmap_size, temp_store, and cache_size all
// reset to SQLite defaults on every new connection, unlike journal_mode (a
// database-level property persisted once in the file header, so it only needs
// to be set a single time below).
//
// modernc.org/sqlite v1.57.0 re-parses and re-applies its `_pragma=name(value)`
// DSN query parameters every time it opens a new connection (see that module's
// driver.go, Driver.Open doc comment, and conn.go's newConn -> applyQueryParams
// call), so encoding these in the DSN — instead of a one-time db.ExecContext
// call that only ever reaches whichever single pooled connection database/sql
// happens to hand back — guarantees they are active on every connection
// database/sql opens as the pool grows under load.
const perConnectionPragmaParams = "_pragma=foreign_keys(1)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_pragma=journal_size_limit(67108864)" +
	"&_pragma=mmap_size(268435456)" +
	"&_pragma=temp_store(MEMORY)" +
	"&_pragma=cache_size(-64000)"

// withPerConnectionPragmas appends the per-connection PRAGMA DSN parameters to
// dsn, merging with any query string dsn already carries (e.g. test DSNs like
// "file:x?mode=memory&cache=shared").
func withPerConnectionPragmas(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + perConnectionPragmaParams
}

// Open initializes a SQLite database connection with production WAL pragmas and executes migrations.
func Open(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", withPerConnectionPragmas(dsn))
	if err != nil {
		return nil, fmt.Errorf("store: failed to open sqlite database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// journal_mode is a database-level property stored in the file header, so
	// setting it once here (on whichever connection database/sql hands back
	// first) is sufficient -- it does not reset per-connection like the
	// pragmas above.
	const journalModePragma = "PRAGMA journal_mode = WAL;"
	if _, err := db.ExecContext(ctx, journalModePragma); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: failed to execute pragma %q: %w", journalModePragma, err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(1 * time.Hour)
	db.SetConnMaxIdleTime(15 * time.Minute)

	// Run embedded migrations
	if err := RunMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: failed to run migrations: %w", err)
	}

	store := newStoreWithExecutor(db, false)
	store.db = db
	return store, nil
}

func newStoreWithExecutor(exec dbExecutor, isTx bool) *SQLiteStore {
	s := &SQLiteStore{
		isTx: isTx,
	}

	s.orgs = &sqlOrgStore{db: exec}
	s.users = &sqlUserStore{db: exec}
	s.sessions = &sqlSessionStore{db: exec}
	s.tokens = &sqlAPITokenStore{db: exec}
	s.projects = &sqlProjectStore{db: exec}
	s.stages = &sqlStageStore{db: exec}
	s.services = &sqlServiceStore{db: exec}
	s.envVars = &sqlEnvVarStore{db: exec}
	s.volumes = &sqlVolumeStore{db: exec}
	s.stacks = &sqlStackStore{db: exec}
	s.networks = &sqlNetworkStore{db: exec}
	s.deployments = &sqlDeploymentStore{db: exec}
	s.backups = &sqlBackupStore{db: exec}
	s.audit = &sqlAuditStore{db: exec}
	s.builds = &sqlBuildStore{db: exec}
	s.githubInstallations = &sqlGitHubInstallationStore{db: exec}
	s.schedules = &sqlScheduleStore{db: exec}
	s.machines = &sqlMachineStore{db: exec}
	s.notifications = &sqlNotificationStore{db: exec}

	return s
}

func (s *SQLiteStore) Organizations() OrganizationStore             { return s.orgs }
func (s *SQLiteStore) Users() UserStore                             { return s.users }
func (s *SQLiteStore) Sessions() SessionStore                       { return s.sessions }
func (s *SQLiteStore) APITokens() APITokenStore                     { return s.tokens }
func (s *SQLiteStore) Projects() ProjectStore                       { return s.projects }
func (s *SQLiteStore) Stages() StageStore                           { return s.stages }
func (s *SQLiteStore) Services() ServiceStore                       { return s.services }
func (s *SQLiteStore) EnvVars() EnvVarStore                         { return s.envVars }
func (s *SQLiteStore) Volumes() VolumeStore                         { return s.volumes }
func (s *SQLiteStore) Stacks() StackStore                           { return s.stacks }
func (s *SQLiteStore) Networks() NetworkStore                       { return s.networks }
func (s *SQLiteStore) Deployments() DeploymentStore                 { return s.deployments }
func (s *SQLiteStore) Backups() BackupStore                         { return s.backups }
func (s *SQLiteStore) Audit() AuditStore                            { return s.audit }
func (s *SQLiteStore) Builds() BuildStore                           { return s.builds }
func (s *SQLiteStore) GitHubInstallations() GitHubInstallationStore { return s.githubInstallations }
func (s *SQLiteStore) Schedules() ScheduleStore                     { return s.schedules }
func (s *SQLiteStore) Machines() MachineStore                       { return s.machines }
func (s *SQLiteStore) Notifications() NotificationStore             { return s.notifications }

func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
	if s.isTx {
		// Already in a transaction
		return fn(s)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: failed to begin transaction: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	txStore := newStoreWithExecutor(tx, true)
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: failed to commit transaction: %w", err)
	}

	return nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
