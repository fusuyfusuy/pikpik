package telemetry_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	err = store.RunMigrations(context.Background(), db)
	require.NoError(t, err)

	return db
}

// TestDownsamplerHourlyRollupAndPersistence tests aggregate computation and persistence in SQLite.
func TestDownsamplerHourlyRollupAndPersistence(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	downsampler := telemetry.NewDownsampler(db)
	ring := telemetry.NewRingBuffer(360)

	now := time.Now().Truncate(time.Hour).Unix()

	// Fill with 360 points (1 hour @ 10s resolution)
	for i := 0; i < 360; i++ {
		ring.Push(telemetry.MetricPoint{
			Timestamp:     now + int64(i*10),
			CPUPercent:    float32((i % 50) + 1), // 1.0 to 50.0
			MemoryBytes:   500 * 1024 * 1024,
			NetRxRate:     1024,
			NetTxRate:     2048,
			DiskReadRate:  512,
			DiskWriteRate: 1024,
		})
	}

	ctx := context.Background()

	// Downsample and save
	err := downsampler.DownsampleAndSave(ctx, "node", "node-alpha-1", ring, now)
	require.NoError(t, err)

	// Query saved rollups
	fromTime := time.Unix(now-10, 0)
	toTime := time.Unix(now+3700, 0)
	results, err := downsampler.QueryHourlyMetrics(ctx, "node", "node-alpha-1", fromTime, toTime)
	require.NoError(t, err)
	require.Len(t, results, 1)

	agg := results[0]
	assert.Equal(t, "node", agg.EntityType)
	assert.Equal(t, "node-alpha-1", agg.EntityID)
	assert.Equal(t, 360, agg.SampleCount)
	assert.Equal(t, 1.0, agg.CPUMin)
	assert.Equal(t, 50.0, agg.CPUMax)
	assert.InDelta(t, 24.944, agg.CPUAvg, 0.01)
	assert.Equal(t, uint64(500*1024*1024), agg.MemAvg)
	assert.Equal(t, uint64(1024*10*360), agg.NetRxTotal)
	assert.Equal(t, uint64(2048*10*360), agg.NetTxTotal)
	assert.Equal(t, uint64(512*10*360), agg.DiskReadTotal)
	assert.Equal(t, uint64(1024*10*360), agg.DiskWriteTotal)

	// Test pruning
	pruned, err := downsampler.PruneMetricsOlderThan(ctx, time.Unix(now+7200, 0))
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned)

	resultsAfterPrune, err := downsampler.QueryHourlyMetrics(ctx, "node", "node-alpha-1", fromTime, toTime)
	require.NoError(t, err)
	assert.Empty(t, resultsAfterPrune)
}

// TestDownsampler_EliminatesDuplicateInsertions verifies multiple saves for the same hour window update in-place without duplicate rows.
func TestDownsampler_EliminatesDuplicateInsertions(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	downsampler := telemetry.NewDownsampler(db)
	ring := telemetry.NewRingBuffer(360)
	now := time.Now().Truncate(time.Hour).Unix()

	ctx := context.Background()

	// 1. Initial push of 10 points
	for i := 0; i < 10; i++ {
		ring.Push(telemetry.MetricPoint{
			Timestamp:   now + int64(i*10),
			CPUPercent:  10.0,
			MemoryBytes: 100 * 1024 * 1024,
		})
	}
	err := downsampler.DownsampleAndSave(ctx, "service", "svc_web", ring, now)
	require.NoError(t, err)

	fromTime := time.Unix(now-10, 0)
	toTime := time.Unix(now+3700, 0)
	results, err := downsampler.QueryHourlyMetrics(ctx, "service", "svc_web", fromTime, toTime)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 10, results[0].SampleCount)

	// 2. Second downsample tick during the same hour with 20 points
	for i := 10; i < 20; i++ {
		ring.Push(telemetry.MetricPoint{
			Timestamp:   now + int64(i*10),
			CPUPercent:  20.0,
			MemoryBytes: 200 * 1024 * 1024,
		})
	}
	err = downsampler.DownsampleAndSave(ctx, "service", "svc_web", ring, now)
	require.NoError(t, err)

	// Verify still exactly 1 record with updated sample count and metrics
	results, err = downsampler.QueryHourlyMetrics(ctx, "service", "svc_web", fromTime, toTime)
	require.NoError(t, err)
	require.Len(t, results, 1, "must not create duplicate row on multiple downsamples within the same hour")
	assert.Equal(t, 20, results[0].SampleCount)

	// 3. Test PruneExpiredMetrics
	pruned, err := downsampler.PruneExpiredMetrics(ctx, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pruned)
}
