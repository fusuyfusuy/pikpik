package s3

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	// Timestamp patterns in backup filenames: 2026-08-30T17-30-00Z or 2026-08-30T17:30:00Z
	tsRegexWithDashes = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}(?:Z|[+-]\d{2}-\d{2})?)`)
	tsRegexWithColons = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)`)
)

// RetentionEngineOptions configures the retention engine.
type RetentionEngineOptions struct {
	ReferenceTime time.Time
}

// RetentionEngine computes which backups to retain and which to prune according to GFS policies.
type RetentionEngine struct {
	refTime time.Time
}

// NewRetentionEngine creates a RetentionEngine instance.
func NewRetentionEngine(opts RetentionEngineOptions) *RetentionEngine {
	ref := opts.ReferenceTime
	if ref.IsZero() {
		ref = time.Now().UTC()
	}
	return &RetentionEngine{
		refTime: ref.UTC(),
	}
}

type backupItem struct {
	info ObjectInfo
	t    time.Time
}

// EvaluateRetention evaluates objects under a prefix and returns keys to delete and keys to retain.
func (e *RetentionEngine) EvaluateRetention(objects []ObjectInfo, policy RetentionPolicy) (toDelete []string, toRetain []string) {
	if len(objects) == 0 {
		return nil, nil
	}

	items := make([]backupItem, 0, len(objects))
	for _, obj := range objects {
		t := parseBackupTimestamp(obj.Key, obj.LastModified)
		items = append(items, backupItem{
			info: obj,
			t:    t,
		})
	}

	// Sort newest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].t.After(items[j].t)
	})

	now := e.refTime
	retainedMap := make(map[string]bool)

	// Hourly bucket
	if policy.KeepHourly > 0 {
		hourlyBuckets := make(map[string]bool)
		hourlyCutoff := now.Add(-time.Duration(policy.KeepHourly) * time.Hour)
		for _, it := range items {
			if it.t.After(hourlyCutoff) || it.t.Equal(hourlyCutoff) {
				hourKey := it.t.Format("2006-01-02-15")
				if !hourlyBuckets[hourKey] {
					hourlyBuckets[hourKey] = true
					retainedMap[it.info.Key] = true
				}
			}
		}
	}

	// Daily bucket
	if policy.KeepDaily > 0 {
		dailyBuckets := make(map[string]bool)
		dailyCutoff := now.AddDate(0, 0, -policy.KeepDaily)
		for _, it := range items {
			if it.t.After(dailyCutoff) || it.t.Equal(dailyCutoff) {
				dayKey := it.t.Format("2006-01-02")
				if !dailyBuckets[dayKey] {
					dailyBuckets[dayKey] = true
					retainedMap[it.info.Key] = true
				}
			}
		}
	}

	// Weekly bucket
	if policy.KeepWeekly > 0 {
		weeklyBuckets := make(map[string]bool)
		weeklyCutoff := now.AddDate(0, 0, -policy.KeepWeekly*7)
		for _, it := range items {
			if it.t.After(weeklyCutoff) || it.t.Equal(weeklyCutoff) {
				year, week := it.t.ISOWeek()
				weekKey := fmt.Sprintf("%04d-W%02d", year, week)
				if !weeklyBuckets[weekKey] {
					weeklyBuckets[weekKey] = true
					retainedMap[it.info.Key] = true
				}
			}
		}
	}

	// Monthly bucket
	if policy.KeepMonthly > 0 {
		monthlyBuckets := make(map[string]bool)
		monthlyCutoff := now.AddDate(0, -policy.KeepMonthly, 0)
		for _, it := range items {
			if it.t.After(monthlyCutoff) || it.t.Equal(monthlyCutoff) {
				monthKey := it.t.Format("2006-01")
				if !monthlyBuckets[monthKey] {
					monthlyBuckets[monthKey] = true
					retainedMap[it.info.Key] = true
				}
			}
		}
	}

	// If no retention rules were active, default to retaining all
	if policy.KeepHourly == 0 && policy.KeepDaily == 0 && policy.KeepWeekly == 0 && policy.KeepMonthly == 0 {
		for _, it := range items {
			retainedMap[it.info.Key] = true
		}
	}

	// Build retained items list (ordered newest first)
	var retainedList []string
	for _, it := range items {
		if retainedMap[it.info.Key] {
			retainedList = append(retainedList, it.info.Key)
		}
	}

	// Enforce MaxBackups safeguard limit
	if policy.MaxBackups > 0 && len(retainedList) > policy.MaxBackups {
		// Truncate to the newest MaxBackups
		for i := policy.MaxBackups; i < len(retainedList); i++ {
			delete(retainedMap, retainedList[i])
		}
		retainedList = retainedList[:policy.MaxBackups]
	}

	// Build delete list
	var deleteList []string
	for _, it := range items {
		if !retainedMap[it.info.Key] {
			deleteList = append(deleteList, it.info.Key)
		}
	}

	return deleteList, retainedList
}

func parseBackupTimestamp(key string, fallback time.Time) time.Time {
	base := filepath.Base(key)

	// Check colon format
	if match := tsRegexWithColons.FindString(base); match != "" {
		if t, err := time.Parse(time.RFC3339, match); err == nil {
			return t.UTC()
		}
	}

	// Check dash format: 2026-08-30T17-30-00Z -> 2026-08-30T17:30:00Z
	if match := tsRegexWithDashes.FindString(base); match != "" {
		normalized := strings.Replace(match, "-", ":", 2) // only replace the time portion dashes
		// Match parts: YYYY-MM-DD + T + HH-MM-SS
		parts := strings.Split(match, "T")
		if len(parts) == 2 {
			timePart := strings.ReplaceAll(parts[1], "-", ":")
			if strings.HasSuffix(timePart, ":Z") {
				timePart = strings.TrimSuffix(timePart, ":Z") + "Z"
			}
			iso := parts[0] + "T" + timePart
			if t, err := time.Parse(time.RFC3339, iso); err == nil {
				return t.UTC()
			}
		}
		_ = normalized
	}

	if !fallback.IsZero() {
		return fallback.UTC()
	}
	return time.Now().UTC()
}
