package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
)

// CronExpression represents a parsed 5-field cron schedule.
type CronExpression struct {
	expr         string
	minutes      [60]bool
	hours        [24]bool
	days         [32]bool // 1-31
	months       [13]bool // 1-12
	weekdays     [7]bool  // 0-6 (0=Sun)
	domRestricted bool
	dowRestricted bool
}

// ParseCron parses a 5-field cron expression or standard macro into a CronExpression.
func ParseCron(expr string) (*CronExpression, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errors.New("empty cron expression")
	}

	// Expand standard cron macros
	switch strings.ToLower(expr) {
	case "@yearly", "@annually":
		expr = "0 0 1 1 *"
	case "@monthly":
		expr = "0 0 1 * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@hourly":
		expr = "0 * * * *"
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 cron fields, got %d in %q", len(fields), expr)
	}

	c := &CronExpression{expr: expr}

	if err := parseCronField(fields[0], 0, 59, c.minutes[:]); err != nil {
		return nil, fmt.Errorf("invalid minute field %q: %w", fields[0], err)
	}
	if err := parseCronField(fields[1], 0, 23, c.hours[:]); err != nil {
		return nil, fmt.Errorf("invalid hour field %q: %w", fields[1], err)
	}
	if err := parseCronField(fields[2], 1, 31, c.days[:]); err != nil {
		return nil, fmt.Errorf("invalid day-of-month field %q: %w", fields[2], err)
	}
	if err := parseCronField(fields[3], 1, 12, c.months[:]); err != nil {
		return nil, fmt.Errorf("invalid month field %q: %w", fields[3], err)
	}
	if err := parseCronField(fields[4], 0, 7, c.weekdays[:]); err != nil {
		return nil, fmt.Errorf("invalid day-of-week field %q: %w", fields[4], err)
	}

	c.domRestricted = fields[2] != "*"
	c.dowRestricted = fields[4] != "*"

	return c, nil
}

func parseCronField(field string, minVal, maxVal int, target []bool) error {
	if field == "*" {
		for i := minVal; i <= maxVal && i < len(target); i++ {
			if minVal == 0 && maxVal == 7 && i == 7 {
				target[0] = true
				continue
			}
			target[i] = true
		}
		return nil
	}

	items := strings.Split(field, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			return errors.New("empty item in field list")
		}

		var step = 1
		rangePart := item

		if strings.Contains(item, "/") {
			parts := strings.SplitN(item, "/", 2)
			rangePart = parts[0]
			s, err := strconv.Atoi(parts[1])
			if err != nil || s <= 0 {
				return fmt.Errorf("invalid step value in %q", item)
			}
			step = s
		}

		var start, end int
		if rangePart == "*" || rangePart == "" {
			start = minVal
			end = maxVal
			if minVal == 0 && maxVal == 7 {
				end = 6
			}
		} else if strings.Contains(rangePart, "-") {
			parts := strings.SplitN(rangePart, "-", 2)
			s, err1 := strconv.Atoi(parts[0])
			e, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil || s > e {
				return fmt.Errorf("invalid range in %q", rangePart)
			}
			start = s
			end = e
		} else {
			val, err := strconv.Atoi(rangePart)
			if err != nil {
				return fmt.Errorf("invalid integer in %q", rangePart)
			}
			start = val
			end = val
		}

		if start < minVal || end > maxVal {
			return fmt.Errorf("value out of range [%d, %d]: %d-%d", minVal, maxVal, start, end)
		}

		for i := start; i <= end; i += step {
			idx := i
			if minVal == 0 && maxVal == 7 && idx == 7 {
				idx = 0
			}
			if idx < len(target) {
				target[idx] = true
			}
		}
	}

	return nil
}

// Matches checks if the given time satisfies the cron expression.
func (c *CronExpression) Matches(t time.Time) bool {
	t = t.UTC()
	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := int(t.Month())
	weekday := int(t.Weekday())

	if !c.minutes[minute] || !c.hours[hour] || !c.months[month] {
		return false
	}

	if c.domRestricted && c.dowRestricted {
		return c.days[day] || c.weekdays[weekday]
	} else if c.domRestricted {
		return c.days[day]
	} else if c.dowRestricted {
		return c.weekdays[weekday]
	}

	return true
}

// Next calculates the earliest time after `from` that matches the cron schedule.
func (c *CronExpression) Next(from time.Time) time.Time {
	t := from.UTC().Truncate(time.Minute).Add(time.Minute)

	// Search up to 5 years into future
	maxLimit := t.AddDate(5, 0, 0)
	for t.Before(maxLimit) {
		month := int(t.Month())
		if !c.months[month] {
			// Skip to start of next month
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}

		day := t.Day()
		weekday := int(t.Weekday())
		dayMatches := false
		if c.domRestricted && c.dowRestricted {
			dayMatches = c.days[day] || c.weekdays[weekday]
		} else if c.domRestricted {
			dayMatches = c.days[day]
		} else if c.dowRestricted {
			dayMatches = c.weekdays[weekday]
		} else {
			dayMatches = true
		}

		if !dayMatches {
			// Skip to start of next day
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
			continue
		}

		hour := t.Hour()
		if !c.hours[hour] {
			// Skip to start of next hour
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		}

		minute := t.Minute()
		if !c.minutes[minute] {
			t = t.Add(time.Minute)
			continue
		}

		return t
	}

	return time.Time{}
}

// CronSchedulerConfig holds configuration options for the backup cron scheduler.
type CronSchedulerConfig struct {
	PollInterval time.Duration
	JobTimeout   time.Duration
	Vault        crypto.Vault
}

// CronScheduler coordinates recurring multi-database backups and GFS retention pruning.
type CronScheduler struct {
	store        store.Store
	backupEngine BackupEngine
	s3Client     s3.S3Client
	vault        crypto.Vault
	pollInterval time.Duration
	jobTimeout   time.Duration

	running     atomic.Bool
	stopCh      chan struct{}
	doneCh      chan struct{}
	lifecycleMu sync.Mutex
	mu          sync.Mutex

	// OnJobCompleted is an optional hook for tests/telemetry.
	OnJobCompleted func(scheduleID string, result *BackupResult, err error)
}

// NewCronScheduler initializes a new CronScheduler.
func NewCronScheduler(st store.Store, engine BackupEngine, s3Client s3.S3Client, cfgs ...CronSchedulerConfig) *CronScheduler {
	pollInterval := 10 * time.Second
	jobTimeout := 15 * time.Minute
	var vault crypto.Vault
	if len(cfgs) > 0 {
		if cfgs[0].PollInterval > 0 {
			pollInterval = cfgs[0].PollInterval
		}
		if cfgs[0].JobTimeout > 0 {
			jobTimeout = cfgs[0].JobTimeout
		}
		vault = cfgs[0].Vault
	}

	return &CronScheduler{
		store:        st,
		backupEngine: engine,
		s3Client:     s3Client,
		vault:        vault,
		pollInterval: pollInterval,
		jobTimeout:   jobTimeout,
	}
}

// Start boots the background polling ticker loop.
func (s *CronScheduler) Start(ctx context.Context) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.running.Swap(true) {
		return errors.New("scheduler already running")
	}

	// Prune orphaned S3 multipart uploads on boot
	if s.s3Client != nil {
		go func() {
			_, _ = s.s3Client.PruneStaleMultipartUploads(ctx, 24*time.Hour)
		}()
	}

	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	stopCh := s.stopCh
	doneCh := s.doneCh

	go func() {
		defer s.running.Store(false)
		defer close(doneCh)
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case now := <-ticker.C:
				_, _ = s.RunDueJobs(ctx, now)
			}
		}
	}()

	return nil
}

// Stop gracefully terminates the scheduler loop.
func (s *CronScheduler) Stop() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if !s.running.Swap(false) {
		return
	}
	close(s.stopCh)
	<-s.doneCh
}

// IsRunning returns whether the scheduler loop is currently active.
func (s *CronScheduler) IsRunning() bool {
	return s.running.Load()
}

// SetVault sets or updates the crypto vault used for secret decryption.
func (s *CronScheduler) SetVault(vault crypto.Vault) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.vault = vault
}

// Trigger immediately runs all currently due backup jobs without waiting for ticker.
func (s *CronScheduler) Trigger(ctx context.Context) ([]*BackupResult, []error) {
	return s.RunDueJobs(ctx, time.Now().UTC())
}

// RunDueJobs queries all active due backup schedules from the database and executes them.
func (s *CronScheduler) RunDueJobs(ctx context.Context, now time.Time) ([]*BackupResult, []error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil || s.store.Schedules() == nil {
		return nil, []error{errors.New("scheduler store not configured")}
	}

	dueSchedules, err := s.store.Schedules().ListDue(ctx, now)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to list due backup schedules: %w", err)}
	}

	var results []*BackupResult
	var errs []error

	for _, sch := range dueSchedules {
		res, execErr := s.ExecuteScheduleJob(ctx, sch, now)
		if execErr != nil {
			errs = append(errs, execErr)
		} else {
			results = append(results, res)
		}

		if s.OnJobCompleted != nil {
			s.OnJobCompleted(sch.ID, res, execErr)
		}
	}

	return results, errs
}

// ExecuteScheduleJob runs a single scheduled backup job, updates schedule timestamps, records execution, and prunes retention.
func (s *CronScheduler) ExecuteScheduleJob(ctx context.Context, sch *store.BackupSchedule, now time.Time) (*BackupResult, error) {
	cron, err := ParseCron(sch.CronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q for schedule %s: %w", sch.CronExpr, sch.ID, err)
	}

	nextRun := cron.Next(now)

	// Fetch associated service to resolve project slug, service slug, container name
	var projectSlug = "default"
	var serviceSlug = "database"
	var containerID = "pikpik_cnt_" + sch.ServiceID

	if sch.ServiceID != "" && s.store != nil {
		svc, err := s.store.Services().GetByID(ctx, sch.ServiceID)
		if err == nil && svc != nil {
			serviceSlug = svc.Slug
			containerID = fmt.Sprintf("pikpik_cnt_%s_%s", svc.ProjectID, svc.Slug)
			proj, pErr := s.store.Projects().GetByID(ctx, svc.ProjectID)
			if pErr == nil && proj != nil {
				projectSlug = proj.Slug
			}
		}
	}

	// Decrypt database password and S3 secret if encrypted and vault is present
	password := sch.PasswordEncrypted
	if s.vault != nil && strings.HasPrefix(password, "v1:") {
		if dec, err := s.vault.DecryptString(ctx, password); err == nil {
			password = dec
		}
	}

	s3SecretKey := sch.S3SecretKeyEncrypted
	if s.vault != nil && strings.HasPrefix(s3SecretKey, "v1:") {
		if dec, err := s.vault.DecryptString(ctx, s3SecretKey); err == nil {
			s3SecretKey = dec
		}
	}

	// Resolve per-schedule S3 client if custom credentials/endpoint/bucket are provided
	var scheduleS3Client s3.S3Client = s.s3Client
	if sch.S3AccessKey != "" || sch.S3Endpoint != "" || s3SecretKey != "" || (s.s3Client == nil && sch.S3Bucket != "") {
		opts := s3.ClientOptions{
			Bucket:          sch.S3Bucket,
			Endpoint:        sch.S3Endpoint,
			Region:          sch.S3Region,
			AccessKeyID:     sch.S3AccessKey,
			SecretAccessKey: s3SecretKey,
		}
		if strings.Contains(sch.S3Endpoint, "r2.cloudflarestorage.com") {
			opts.Provider = s3.ProviderR2
		} else if strings.Contains(sch.S3Endpoint, "backblazeb2.com") {
			opts.Provider = s3.ProviderBackblaze
		} else if sch.S3Endpoint != "" {
			opts.Provider = s3.ProviderMinIO
			opts.ForcePathStyle = true
		} else {
			opts.Provider = s3.ProviderAWS
		}
		if cli, err := s3.NewClient(opts); err == nil {
			scheduleS3Client = cli
		}
	}

	// Generate random backup ID
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	backupID := "bk_" + hex.EncodeToString(b)

	jobCfg := BackupJobConfig{
		BackupID:       backupID,
		ProjectSlug:    projectSlug,
		ServiceSlug:    serviceSlug,
		ContainerID:    containerID,
		Engine:         DatabaseEngine(sch.Engine),
		DatabaseName:   sch.DatabaseName,
		Username:       sch.Username,
		Password:       password,
		S3Bucket:       sch.S3Bucket,
		S3Endpoint:     sch.S3Endpoint,
		S3Region:       sch.S3Region,
		S3AccessKey:    sch.S3AccessKey,
		S3SecretKey:    s3SecretKey,
		S3Client:       scheduleS3Client,
		Compression:    sch.Compression,
		RetentionRules: RetentionRules{
			KeepHourly:  sch.RetentionHourly,
			KeepDaily:   sch.RetentionDaily,
			KeepWeekly:  sch.RetentionWeekly,
			KeepMonthly: sch.RetentionMonthly,
			MaxBackups:  sch.MaxBackups,
		},
	}

	if s.backupEngine == nil {
		return nil, errors.New("backup engine is not configured")
	}

	jobTimeout := s.jobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 15 * time.Minute
	}
	jobCtx, jobCancel := context.WithTimeout(ctx, jobTimeout)
	defer jobCancel()

	startTime := time.Now()
	res, backupErr := s.backupEngine.StreamBackup(jobCtx, jobCfg)
	duration := time.Since(startTime)

	// Update schedule run times
	_ = s.store.Schedules().UpdateRunTimes(ctx, sch.ID, now, nextRun)

	// Ensure configID resolves to a valid backup_config record for execution logging
	configID := sch.ID
	if sch.ServiceID != "" && s.store != nil {
		bkpCfg, err := s.store.Backups().GetConfigByService(ctx, sch.ServiceID)
		if err == nil && bkpCfg != nil {
			configID = bkpCfg.ID
		} else {
			s3SecretEnc := sch.S3SecretKeyEncrypted
			if s3SecretEnc != "" && s.vault != nil && !strings.HasPrefix(s3SecretEnc, "v1:") {
				if enc, err := s.vault.EncryptString(ctx, s3SecretEnc); err == nil {
					s3SecretEnc = enc
				}
			}
			newCfg := &store.BackupConfig{
				ID:                   "bkp_" + sch.ID,
				ServiceID:            sch.ServiceID,
				S3Endpoint:           sch.S3Endpoint,
				S3Bucket:             sch.S3Bucket,
				S3Region:             sch.S3Region,
				S3AccessKey:          sch.S3AccessKey,
				S3SecretKeyEncrypted: s3SecretEnc,
				CronExpr:             sch.CronExpr,
				RetentionDays:        30,
				IsEnabled:            sch.IsEnabled,
			}
			if cErr := s.store.Backups().CreateConfig(ctx, newCfg); cErr == nil {
				configID = newCfg.ID
			}
		}
	}

	// Record execution log in database
	execRecord := &store.BackupExecution{
		ID:         "bke_" + hex.EncodeToString(b),
		ConfigID:   configID,
		ServiceID:  sch.ServiceID,
		Status:     "completed",
		DurationMS: duration.Milliseconds(),
		CreatedAt:  now.UTC(),
	}

	if backupErr != nil {
		execRecord.Status = "failed"
		execRecord.ErrorMessage = backupErr.Error()
	} else if res != nil {
		execRecord.S3Key = res.S3Key
		execRecord.BytesStreamed = res.CompressedBytes
		execRecord.DurationMS = res.DurationMs
	}

	_ = s.store.Backups().RecordExecution(ctx, execRecord)

	if backupErr != nil {
		return nil, fmt.Errorf("backup execution failed for schedule %s: %w", sch.ID, backupErr)
	}

	// Trigger S3 GFS retention pruning if S3 client is available
	if scheduleS3Client != nil && (sch.RetentionHourly > 0 || sch.RetentionDaily > 0 || sch.RetentionWeekly > 0 || sch.RetentionMonthly > 0 || sch.MaxBackups > 0) {
		prefix := fmt.Sprintf("backups/%s/%s/", projectSlug, serviceSlug)
		_, _ = scheduleS3Client.PruneRetention(jobCtx, prefix, s3.RetentionPolicy{
			KeepHourly:  sch.RetentionHourly,
			KeepDaily:   sch.RetentionDaily,
			KeepWeekly:  sch.RetentionWeekly,
			KeepMonthly: sch.RetentionMonthly,
			MaxBackups:  sch.MaxBackups,
		})
	}

	return res, nil
}
