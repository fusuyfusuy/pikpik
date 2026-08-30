package backup_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func newSchedulerTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sched_test.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func TestCronParser_ExpressionsAndMacros(t *testing.T) {
	tests := []struct {
		expr    string
		valid   bool
		matches time.Time
		nextIn  time.Duration
	}{
		{
			expr:    "0 * * * *", // Hourly at min 0
			valid:   true,
			matches: time.Date(2026, 8, 30, 14, 0, 0, 0, time.UTC),
			nextIn:  1 * time.Hour,
		},
		{
			expr:    "*/15 * * * *", // Every 15 min
			valid:   true,
			matches: time.Date(2026, 8, 30, 14, 45, 0, 0, time.UTC),
			nextIn:  15 * time.Minute,
		},
		{
			expr:    "30 4 1,15 * *", // 04:30 on 1st and 15th
			valid:   true,
			matches: time.Date(2026, 9, 1, 4, 30, 0, 0, time.UTC),
		},
		{
			expr:    "@daily",
			valid:   true,
			matches: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			expr:    "@hourly",
			valid:   true,
			matches: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		},
		{
			expr:    "@weekly",
			valid:   true,
			matches: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), // 2026-08-30 is Sunday
		},
		{
			expr:  "invalid-cron",
			valid: false,
		},
		{
			expr:  "60 * * * *", // Minute 60 out of range
			valid: false,
		},
		{
			expr:  "* 24 * * *", // Hour 24 out of range
			valid: false,
		},
		{
			expr:  "* * 32 * *", // Day 32 out of range
			valid: false,
		},
		{
			expr:  "* * * 13 *", // Month 13 out of range
			valid: false,
		},
		{
			expr:  "* * * * 8", // Weekday 8 out of range
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			c, err := backup.ParseCron(tc.expr)
			if !tc.valid {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, c)

			if !tc.matches.IsZero() {
				assert.True(t, c.Matches(tc.matches), "Expected %v to match %s", tc.matches, tc.expr)
			}
		})
	}
}

func TestCronParser_NextCalculation(t *testing.T) {
	c, err := backup.ParseCron("0 2 * * *") // Daily at 02:00 UTC
	require.NoError(t, err)

	refTime := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	next := c.Next(refTime)
	expected := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, next)

	// Past 02:00 on the same day -> should return 02:00 tomorrow
	refTimeAfter := time.Date(2026, 8, 30, 3, 0, 0, 0, time.UTC)
	nextAfter := c.Next(refTimeAfter)
	expectedAfter := time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC)
	assert.Equal(t, expectedAfter, nextAfter)
}

type mockSchedulerBackupEngine struct {
	mu           sync.Mutex
	lastJobCfg   backup.BackupJobConfig
	returnResult *backup.BackupResult
	returnErr    error
	calledCount  int
}

func (m *mockSchedulerBackupEngine) StreamBackup(ctx context.Context, cfg backup.BackupJobConfig) (*backup.BackupResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calledCount++
	m.lastJobCfg = cfg
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	if m.returnResult != nil {
		return m.returnResult, nil
	}
	return &backup.BackupResult{
		BackupID:        cfg.BackupID,
		S3Key:           "backups/test/db/2026-08-30T00-00-00Z_postgres17_" + cfg.BackupID + ".dump.gz",
		CompressedBytes: 1024,
		DurationMs:      50,
		CreatedAt:       time.Now().UTC(),
		Engine:          cfg.Engine,
	}, nil
}

func (m *mockSchedulerBackupEngine) StreamRestore(ctx context.Context, cfg backup.RestoreJobConfig) error {
	return nil
}

func (m *mockSchedulerBackupEngine) VerifyBackupEphemeral(ctx context.Context, cfg backup.RestoreJobConfig) (bool, error) {
	return true, nil
}

func TestCronScheduler_RunDueJobs_Success(t *testing.T) {
	ctx := context.Background()
	st := newSchedulerTestStore(t)

	// Setup tenant hierarchy in store
	org := &store.Organization{Name: "Org", Slug: "org"}
	require.NoError(t, st.Organizations().Create(ctx, org))
	proj := &store.Project{OrgID: org.ID, Name: "Proj", Slug: "proj"}
	require.NoError(t, st.Projects().Create(ctx, proj))
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	require.NoError(t, st.Stages().Create(ctx, stage))
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Postgres DB",
		Slug:      "postgres-db",
		Type:      "database",
		Image:     "postgres:17-alpine",
	}
	require.NoError(t, st.Services().Create(ctx, svc))

	// Create due backup schedule (NextRunAt = past)
	pastTime := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	sch := &store.BackupSchedule{
		ServiceID:        svc.ID,
		CronExpr:         "0 2 * * *",
		Engine:           "postgres:17",
		DatabaseName:     "app_prod",
		Username:         "pguser",
		S3Bucket:         "backups-bucket",
		RetentionHourly:  24,
		RetentionDaily:   7,
		RetentionWeekly:  4,
		RetentionMonthly: 12,
		MaxBackups:       30,
		Compression:      "gzip",
		IsEnabled:        true,
		NextRunAt:        &pastTime,
	}
	require.NoError(t, st.Schedules().Create(ctx, sch))

	mockEngine := &mockSchedulerBackupEngine{}
	mockS3 := &mockS3MultipartClient{storage: make(map[string][]byte)}

	scheduler := backup.NewCronScheduler(st, mockEngine, mockS3)

	// Run due jobs
	now := time.Date(2026, 8, 30, 2, 5, 0, 0, time.UTC)
	results, errs := scheduler.RunDueJobs(ctx, now)
	require.Empty(t, errs)
	require.Len(t, results, 1)

	// Verify engine was invoked with correct config
	assert.Equal(t, 1, mockEngine.calledCount)
	assert.Equal(t, "app_prod", mockEngine.lastJobCfg.DatabaseName)
	assert.Equal(t, backup.EnginePostgres17, mockEngine.lastJobCfg.Engine)
	assert.Equal(t, "proj", mockEngine.lastJobCfg.ProjectSlug)
	assert.Equal(t, "postgres-db", mockEngine.lastJobCfg.ServiceSlug)
	assert.Equal(t, 24, mockEngine.lastJobCfg.RetentionRules.KeepHourly)

	// Verify schedule run times updated
	updatedSch, err := st.Schedules().GetByID(ctx, sch.ID)
	require.NoError(t, err)
	require.NotNil(t, updatedSch.LastRunAt)
	require.NotNil(t, updatedSch.NextRunAt)
	assert.Equal(t, now.UTC(), *updatedSch.LastRunAt)
	assert.Equal(t, time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC), *updatedSch.NextRunAt)

	// Verify execution record logged in DB
	execs, err := st.Backups().ListExecutions(ctx, svc.ID, 10)
	require.NoError(t, err)
	require.Len(t, execs, 1)
	assert.Equal(t, "completed", execs[0].Status)
	assert.Contains(t, execs[0].ConfigID, sch.ID)
	assert.Equal(t, int64(1024), execs[0].BytesStreamed)
}


func TestCronScheduler_RunDueJobs_FailureLogged(t *testing.T) {
	ctx := context.Background()
	st := newSchedulerTestStore(t)

	org := &store.Organization{Name: "Org Fail", Slug: "org-fail"}
	require.NoError(t, st.Organizations().Create(ctx, org))
	proj := &store.Project{OrgID: org.ID, Name: "Proj Fail", Slug: "proj-fail"}
	require.NoError(t, st.Projects().Create(ctx, proj))
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	require.NoError(t, st.Stages().Create(ctx, stage))
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "MySQL Fail",
		Slug:      "mysql-fail",
		Type:      "database",
		Image:     "mysql:8.4",
	}
	require.NoError(t, st.Services().Create(ctx, svc))

	pastTime := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	sch := &store.BackupSchedule{
		ServiceID:    svc.ID,
		CronExpr:     "0 1 * * *",
		Engine:       "mysql:8.4",
		DatabaseName: "mydb",
		S3Bucket:     "backups",
		IsEnabled:    true,
		NextRunAt:    &pastTime,
	}
	require.NoError(t, st.Schedules().Create(ctx, sch))

	mockEngine := &mockSchedulerBackupEngine{
		returnErr: errors.New("container connection refused"),
	}

	scheduler := backup.NewCronScheduler(st, mockEngine, nil)

	now := time.Date(2026, 8, 30, 1, 15, 0, 0, time.UTC)
	results, errs := scheduler.RunDueJobs(ctx, now)
	require.Len(t, errs, 1)
	require.Empty(t, results)
	assert.Contains(t, errs[0].Error(), "container connection refused")

	// Verify execution was logged with failed status
	execs, err := st.Backups().ListExecutions(ctx, sch.ServiceID, 10)
	require.NoError(t, err)
	require.Len(t, execs, 1)
	assert.Equal(t, "failed", execs[0].Status)
	assert.Contains(t, execs[0].ErrorMessage, "container connection refused")
}

func TestCronScheduler_StartStopLifecycle(t *testing.T) {
	st := newSchedulerTestStore(t)
	mockEngine := &mockSchedulerBackupEngine{}

	scheduler := backup.NewCronScheduler(st, mockEngine, nil, backup.CronSchedulerConfig{
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := scheduler.Start(ctx)
	require.NoError(t, err)

	// Second start should return error
	err = scheduler.Start(ctx)
	assert.Error(t, err)

	time.Sleep(50 * time.Millisecond)
	scheduler.Stop()
}
