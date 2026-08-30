package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
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
	db      *sql.DB
	writeMu *sync.Mutex
	isTx    bool

	orgs        OrganizationStore
	users       UserStore
	sessions    SessionStore
	tokens      APITokenStore
	projects    ProjectStore
	stages      StageStore
	services    ServiceStore
	envVars     EnvVarStore
	volumes     VolumeStore
	deployments DeploymentStore
	backups             BackupStore
	audit               AuditStore
	builds              BuildStore
	githubInstallations GitHubInstallationStore
	schedules           ScheduleStore
}

// Open initializes a SQLite database connection with production WAL pragmas and executes migrations.
func Open(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: failed to open sqlite database: %w", err)
	}

	// Apply SQLite PRAGMAs on the master connection
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA journal_size_limit = 67108864;",
		"PRAGMA mmap_size = 268435456;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -64000;",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: failed to execute pragma %q: %w", p, err)
		}
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

	store := newStoreWithExecutor(db, &sync.Mutex{}, false)
	store.db = db
	return store, nil
}

func newStoreWithExecutor(exec dbExecutor, writeMu *sync.Mutex, isTx bool) *SQLiteStore {
	s := &SQLiteStore{
		writeMu: writeMu,
		isTx:    isTx,
	}

	s.orgs = &sqlOrgStore{db: exec, writeMu: writeMu}
	s.users = &sqlUserStore{db: exec, writeMu: writeMu}
	s.sessions = &sqlSessionStore{db: exec, writeMu: writeMu}
	s.tokens = &sqlAPITokenStore{db: exec, writeMu: writeMu}
	s.projects = &sqlProjectStore{db: exec, writeMu: writeMu}
	s.stages = &sqlStageStore{db: exec, writeMu: writeMu}
	s.services = &sqlServiceStore{db: exec, writeMu: writeMu}
	s.envVars = &sqlEnvVarStore{db: exec, writeMu: writeMu}
	s.volumes = &sqlVolumeStore{db: exec, writeMu: writeMu}
	s.deployments = &sqlDeploymentStore{db: exec, writeMu: writeMu}
	s.backups = &sqlBackupStore{db: exec, writeMu: writeMu}
	s.audit = &sqlAuditStore{db: exec, writeMu: writeMu}
	s.builds = &sqlBuildStore{db: exec, writeMu: writeMu}
	s.githubInstallations = &sqlGitHubInstallationStore{db: exec, writeMu: writeMu}
	s.schedules = &sqlScheduleStore{db: exec, writeMu: writeMu}

	return s
}

func (s *SQLiteStore) Organizations() OrganizationStore                 { return s.orgs }
func (s *SQLiteStore) Users() UserStore                                 { return s.users }
func (s *SQLiteStore) Sessions() SessionStore                           { return s.sessions }
func (s *SQLiteStore) APITokens() APITokenStore                         { return s.tokens }
func (s *SQLiteStore) Projects() ProjectStore                           { return s.projects }
func (s *SQLiteStore) Stages() StageStore                               { return s.stages }
func (s *SQLiteStore) Services() ServiceStore                           { return s.services }
func (s *SQLiteStore) EnvVars() EnvVarStore                             { return s.envVars }
func (s *SQLiteStore) Volumes() VolumeStore                             { return s.volumes }
func (s *SQLiteStore) Deployments() DeploymentStore                     { return s.deployments }
func (s *SQLiteStore) Backups() BackupStore                             { return s.backups }
func (s *SQLiteStore) Audit() AuditStore                                { return s.audit }
func (s *SQLiteStore) Builds() BuildStore                               { return s.builds }
func (s *SQLiteStore) GitHubInstallations() GitHubInstallationStore     { return s.githubInstallations }
func (s *SQLiteStore) Schedules() ScheduleStore                         { return s.schedules }

func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) WithTx(ctx context.Context, fn func(tx Store) error) error {
	if s.isTx {
		// Already in a transaction
		return fn(s)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: failed to begin transaction: %w", err)
	}

	txStore := newStoreWithExecutor(tx, s.writeMu, true)
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
