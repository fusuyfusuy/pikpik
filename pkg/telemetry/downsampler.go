package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Downsampler handles computing and persisting hourly statistical rollups to SQLite.
// ponytail: single-node SQLite metrics table <- 100 active containers -> duckdb/parquet offloading when node count > 50
type Downsampler struct {
	db *sql.DB
}

// NewDownsampler initializes a Downsampler with the provided SQLite database connection.
func NewDownsampler(db *sql.DB) *Downsampler {
	return &Downsampler{
		db: db,
	}
}

// DownsampleEntity computes the aggregate for an entity's RingBuffer over a 1-hour window starting at hourStart.
func (d *Downsampler) DownsampleEntity(ctx context.Context, entityType, entityID string, buf RingBuffer, hourStart int64) (*DownsampleAggregate, error) {
	if buf == nil {
		return nil, fmt.Errorf("telemetry: nil ring buffer for %s:%s", entityType, entityID)
	}

	agg, err := buf.DownsampleHour(hourStart)
	if err != nil {
		return nil, err
	}
	if agg == nil {
		return nil, nil // No points in this window
	}

	agg.EntityType = entityType
	agg.EntityID = entityID
	return agg, nil
}

// DownsampleAndSave computes the aggregate and inserts it into SQLite.
func (d *Downsampler) DownsampleAndSave(ctx context.Context, entityType, entityID string, buf RingBuffer, hourStart int64) error {
	agg, err := d.DownsampleEntity(ctx, entityType, entityID, buf, hourStart)
	if err != nil {
		return err
	}
	if agg == nil {
		return nil
	}

	return d.SaveAggregate(ctx, agg)
}

// SaveAggregate inserts a single DownsampleAggregate into SQLite table system_metrics_hourly.
func (d *Downsampler) SaveAggregate(ctx context.Context, agg *DownsampleAggregate) error {
	if d.db == nil {
		return nil
	}

	query := `
	INSERT INTO system_metrics_hourly (
		entity_type, entity_id, bucket_start,
		cpu_avg, cpu_min, cpu_max, cpu_p95,
		mem_avg, mem_max,
		net_rx_total, net_tx_total,
		disk_read_total, disk_write_total,
		sample_count, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	nowUnix := time.Now().UTC().Unix()
	bucketUnix := agg.BucketStart.UTC().Unix()

	_, err := d.db.ExecContext(ctx, query,
		agg.EntityType,
		agg.EntityID,
		bucketUnix,
		agg.CPUAvg,
		agg.CPUMin,
		agg.CPUMax,
		agg.CPUP95,
		agg.MemAvg,
		agg.MemMax,
		agg.NetRxTotal,
		agg.NetTxTotal,
		agg.DiskReadTotal,
		agg.DiskWriteTotal,
		agg.SampleCount,
		nowUnix,
	)
	if err != nil {
		return fmt.Errorf("telemetry: failed to save hourly aggregate: %w", err)
	}
	return nil
}

// QueryHourlyMetrics retrieves hourly rollups for an entity within a time range.
func (d *Downsampler) QueryHourlyMetrics(ctx context.Context, entityType, entityID string, fromTime, toTime time.Time) ([]*DownsampleAggregate, error) {
	if d.db == nil {
		return nil, fmt.Errorf("telemetry: database not configured")
	}

	query := `
	SELECT entity_type, entity_id, bucket_start,
	       cpu_avg, cpu_min, cpu_max, cpu_p95,
	       mem_avg, mem_max,
	       net_rx_total, net_tx_total,
	       disk_read_total, disk_write_total,
	       sample_count
	FROM system_metrics_hourly
	WHERE entity_type = ? AND entity_id = ? AND bucket_start >= ? AND bucket_start <= ?
	ORDER BY bucket_start ASC`

	rows, err := d.db.QueryContext(ctx, query, entityType, entityID, fromTime.Unix(), toTime.Unix())
	if err != nil {
		return nil, fmt.Errorf("telemetry: query hourly metrics failed: %w", err)
	}
	defer rows.Close()

	var results []*DownsampleAggregate
	for rows.Next() {
		var agg DownsampleAggregate
		var bucketStartUnix int64
		if err := rows.Scan(
			&agg.EntityType,
			&agg.EntityID,
			&bucketStartUnix,
			&agg.CPUAvg,
			&agg.CPUMin,
			&agg.CPUMax,
			&agg.CPUP95,
			&agg.MemAvg,
			&agg.MemMax,
			&agg.NetRxTotal,
			&agg.NetTxTotal,
			&agg.DiskReadTotal,
			&agg.DiskWriteTotal,
			&agg.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("telemetry: scan hourly metrics failed: %w", err)
		}
		agg.BucketStart = time.Unix(bucketStartUnix, 0).UTC()
		results = append(results, &agg)
	}

	return results, rows.Err()
}

// PruneMetricsOlderThan deletes rollup records older than the specified retention cutoff.
func (d *Downsampler) PruneMetricsOlderThan(ctx context.Context, olderThan time.Time) (int64, error) {
	if d.db == nil {
		return 0, nil
	}

	query := `DELETE FROM system_metrics_hourly WHERE bucket_start < ?`
	res, err := d.db.ExecContext(ctx, query, olderThan.Unix())
	if err != nil {
		return 0, fmt.Errorf("telemetry: prune old metrics failed: %w", err)
	}
	return res.RowsAffected()
}
