package backup_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/crypto"
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

	assert.False(t, scheduler.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. First Start
	err := scheduler.Start(ctx)
	require.NoError(t, err)
	assert.True(t, scheduler.IsRunning())

	// 2. Second start should return error
	err = scheduler.Start(ctx)
	assert.Error(t, err)

	time.Sleep(30 * time.Millisecond)

	// 3. Stop
	scheduler.Stop()
	assert.False(t, scheduler.IsRunning())

	// 4. Multiple calls to Stop should be safe and no-op
	scheduler.Stop()
	assert.False(t, scheduler.IsRunning())

	// 5. Restart after stop
	err = scheduler.Start(ctx)
	require.NoError(t, err)
	assert.True(t, scheduler.IsRunning())

	scheduler.Stop()
	assert.False(t, scheduler.IsRunning())
}

func TestCronScheduler_TriggerMethod(t *testing.T) {
	ctx := context.Background()
	st := newSchedulerTestStore(t)

	org := &store.Organization{Name: "Org Trigger", Slug: "org-trigger"}
	require.NoError(t, st.Organizations().Create(ctx, org))
	proj := &store.Project{OrgID: org.ID, Name: "Proj Trigger", Slug: "proj-trigger"}
	require.NoError(t, st.Projects().Create(ctx, proj))
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	require.NoError(t, st.Stages().Create(ctx, stage))
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Redis Service",
		Slug:      "redis-svc",
		Type:      "database",
		Image:     "redis:7.4",
	}
	require.NoError(t, st.Services().Create(ctx, svc))

	pastTime := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	sch := &store.BackupSchedule{
		ServiceID:    svc.ID,
		CronExpr:     "0 0 * * *",
		Engine:       "redis:7.4",
		DatabaseName: "dump.rdb",
		S3Bucket:     "trigger-bucket",
		IsEnabled:    true,
		NextRunAt:    &pastTime,
	}
	require.NoError(t, st.Schedules().Create(ctx, sch))

	mockEngine := &mockSchedulerBackupEngine{}
	scheduler := backup.NewCronScheduler(st, mockEngine, nil)

	// Explicit manual trigger
	results, errs := scheduler.Trigger(ctx)
	require.Empty(t, errs)
	require.Len(t, results, 1)
	assert.Equal(t, 1, mockEngine.calledCount)
	assert.Equal(t, "dump.rdb", mockEngine.lastJobCfg.DatabaseName)
}

func TestCronScheduler_PerScheduleS3CredentialsAndVault(t *testing.T) {
	ctx := context.Background()
	st := newSchedulerTestStore(t)

	vault, err := crypto.NewAESVault("master-secret-for-testing-vault-32b!")
	require.NoError(t, err)

	encDbPass, err := vault.EncryptString(ctx, "my-super-db-pass")
	require.NoError(t, err)

	encS3Secret, err := vault.EncryptString(ctx, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	require.NoError(t, err)

	org := &store.Organization{Name: "Org S3", Slug: "org-s3"}
	require.NoError(t, st.Organizations().Create(ctx, org))
	proj := &store.Project{OrgID: org.ID, Name: "Proj S3", Slug: "proj-s3"}
	require.NoError(t, st.Projects().Create(ctx, proj))
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	require.NoError(t, st.Stages().Create(ctx, stage))
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Secure Postgres",
		Slug:      "secure-pg",
		Type:      "database",
		Image:     "postgres:17-alpine",
	}
	require.NoError(t, st.Services().Create(ctx, svc))

	pastTime := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	sch := &store.BackupSchedule{
		ServiceID:            svc.ID,
		CronExpr:             "@daily",
		Engine:               "postgres:17",
		DatabaseName:         "secure_db",
		Username:             "secuser",
		PasswordEncrypted:    encDbPass,
		S3Bucket:             "tenant-isolated-bucket",
		S3Endpoint:           "https://minio.custom-tenant.com:9000",
		S3Region:             "us-west-2",
		S3AccessKey:          "AKIAIOSFODNN7EXAMPLE",
		S3SecretKeyEncrypted: encS3Secret,
		RetentionHourly:      12,
		RetentionDaily:       3,
		MaxBackups:           10,
		IsEnabled:            true,
		NextRunAt:            &pastTime,
	}
	require.NoError(t, st.Schedules().Create(ctx, sch))

	mockEngine := &mockSchedulerBackupEngine{}
	scheduler := backup.NewCronScheduler(st, mockEngine, nil, backup.CronSchedulerConfig{
		Vault: vault,
	})

	now := time.Date(2026, 8, 30, 0, 5, 0, 0, time.UTC)
	results, errs := scheduler.RunDueJobs(ctx, now)
	require.Empty(t, errs)
	require.Len(t, results, 1)

	// Verify decrypted passwords and per-schedule S3 config were passed to engine job config
	assert.Equal(t, 1, mockEngine.calledCount)
	assert.Equal(t, "my-super-db-pass", mockEngine.lastJobCfg.Password)
	assert.Equal(t, "tenant-isolated-bucket", mockEngine.lastJobCfg.S3Bucket)
	assert.Equal(t, "https://minio.custom-tenant.com:9000", mockEngine.lastJobCfg.S3Endpoint)
	assert.Equal(t, "us-west-2", mockEngine.lastJobCfg.S3Region)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", mockEngine.lastJobCfg.S3AccessKey)
	assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", mockEngine.lastJobCfg.S3SecretKey)
	assert.NotNil(t, mockEngine.lastJobCfg.S3Client)
}

type mockSchedulerS3Client struct {
	mu           sync.Mutex
	prunedCount  int
	prunedMaxAge time.Duration
}

func (m *mockSchedulerS3Client) UploadStreamMultipart(ctx context.Context, key string, reader io.Reader, opts s3.UploadOptions) (*s3.ObjectInfo, error) {
	return nil, nil
}
func (m *mockSchedulerS3Client) DownloadStream(ctx context.Context, key string) (io.ReadCloser, *s3.ObjectInfo, error) {
	return nil, nil, nil
}
func (m *mockSchedulerS3Client) ListObjects(ctx context.Context, prefix string) ([]s3.ObjectInfo, error) {
	return nil, nil
}
func (m *mockSchedulerS3Client) DeleteObjects(ctx context.Context, keys []string) error {
	return nil
}
func (m *mockSchedulerS3Client) PruneRetention(ctx context.Context, prefix string, policy s3.RetentionPolicy) ([]string, error) {
	return nil, nil
}
func (m *mockSchedulerS3Client) PruneStaleMultipartUploads(ctx context.Context, maxAge time.Duration) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prunedCount++
	m.prunedMaxAge = maxAge
	return []string{"pruned-1"}, nil
}

func TestCronScheduler_BootTimePruneStaleMultipartUploads(t *testing.T) {
	st := newSchedulerTestStore(t)
	mockEngine := &mockSchedulerBackupEngine{}
	mockS3 := &mockSchedulerS3Client{}

	scheduler := backup.NewCronScheduler(st, mockEngine, mockS3, backup.CronSchedulerConfig{
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := scheduler.Start(ctx)
	require.NoError(t, err)
	defer scheduler.Stop()

	require.Eventually(t, func() bool {
		mockS3.mu.Lock()
		defer mockS3.mu.Unlock()
		return mockS3.prunedCount >= 1 && mockS3.prunedMaxAge == 24*time.Hour
	}, 1*time.Second, 10*time.Millisecond)
}

func TestCronScheduler_ExecutionTimeoutIsolation(t *testing.T) {
	ctx := context.Background()
	st := newSchedulerTestStore(t)

	org := &store.Organization{Name: "Org Timeout", Slug: "org-timeout"}
	require.NoError(t, st.Organizations().Create(ctx, org))
	proj := &store.Project{OrgID: org.ID, Name: "Proj Timeout", Slug: "proj-timeout"}
	require.NoError(t, st.Projects().Create(ctx, proj))
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	require.NoError(t, st.Stages().Create(ctx, stage))
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Timeout DB",
		Slug:      "timeout-db",
		Type:      "database",
		Image:     "postgres:17-alpine",
	}
	require.NoError(t, st.Services().Create(ctx, svc))

	pastTime := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	sch := &store.BackupSchedule{
		ServiceID: svc.ID,
		CronExpr:  "0 2 * * *",
		Engine:    "postgres:17",
		S3Bucket:  "backups-bucket",
		IsEnabled: true,
		NextRunAt: &pastTime,
	}
	require.NoError(t, st.Schedules().Create(ctx, sch))

	// Mock engine that blocks until context is cancelled
	mockEngine := &mockSchedulerBackupEngine{}
	hangEngine := &hangingBackupEngine{}

	scheduler := backup.NewCronScheduler(st, hangEngine, nil, backup.CronSchedulerConfig{
		PollInterval: 10 * time.Millisecond,
		JobTimeout:   50 * time.Millisecond, // Isolated short timeout
	})

	now := time.Date(2026, 8, 30, 2, 5, 0, 0, time.UTC)
	results, errs := scheduler.RunDueJobs(ctx, now)
	require.Len(t, errs, 1, "should return 1 timeout error")
	require.Empty(t, results)
	assert.Contains(t, errs[0].Error(), "context deadline exceeded")

	// Verify failed execution logged in DB
	execs, err := st.Backups().ListExecutions(ctx, svc.ID, 10)
	require.NoError(t, err)
	require.Len(t, execs, 1)
	assert.Equal(t, "failed", execs[0].Status)
	assert.Contains(t, execs[0].ErrorMessage, "context deadline exceeded")
	_ = mockEngine
}

type hangingBackupEngine struct{}

func (h *hangingBackupEngine) StreamBackup(ctx context.Context, cfg backup.BackupJobConfig) (*backup.BackupResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (h *hangingBackupEngine) StreamRestore(ctx context.Context, cfg backup.RestoreJobConfig) error {
	return nil
}
func (h *hangingBackupEngine) VerifyBackupEphemeral(ctx context.Context, cfg backup.RestoreJobConfig) (bool, error) {
	return true, nil
}


