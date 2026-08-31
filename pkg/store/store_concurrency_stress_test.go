package store_test

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdversarial_Store_100Goroutines_MixedTransactionsAndReads stresses SQLite connection pool,
// WAL mode, and write lock handling across 100 concurrent goroutines executing mixed transactions.
func TestAdversarial_Store_100Goroutines_MixedTransactionsAndReads(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "stress_100.db")
	st, err := store.Open(dbPath)
	require.NoError(t, err)
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Seed baseline org
	rootOrg := &store.Organization{
		Name: "Stress Root Org",
		Slug: "stress-root-org",
	}
	require.NoError(t, st.Organizations().Create(context.Background(), rootOrg))

	const numWorkers = 100
	var wg sync.WaitGroup
	var writesCompleted int64
	var readsCompleted int64
	var rollbacksCompleted int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			var opCount int64

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opCount++
					mode := r.Intn(4)

					switch mode {
					case 0, 1: // Write transaction
						slug := fmt.Sprintf("p-%d-%d", workerID, opCount)
						err := st.WithTx(ctx, func(txStore store.Store) error {
							p := &store.Project{
								OrgID: rootOrg.ID,
								Name:  "Prj " + slug,
								Slug:  slug,
							}
							if err := txStore.Projects().Create(ctx, p); err != nil {
								return err
							}
							stage := &store.Stage{
								ProjectID: p.ID,
								Name:      "Prod",
								Slug:      "stage-" + slug,
							}
							return txStore.Stages().Create(ctx, stage)
						})
						if err == nil {
							atomic.AddInt64(&writesCompleted, 1)
						}

					case 2: // Read operations
						_, _ = st.Projects().List(ctx, rootOrg.ID)
						_, _ = st.Organizations().GetBySlug(ctx, "stress-root-org")
						atomic.AddInt64(&readsCompleted, 1)

					case 3: // Intentional rollback transaction
						_ = st.WithTx(ctx, func(txStore store.Store) error {
							p := &store.Project{
								OrgID: rootOrg.ID,
								Name:  fmt.Sprintf("Rollback-%d-%d", workerID, opCount),
								Slug:  fmt.Sprintf("rb-%d-%d", workerID, opCount),
							}
							_ = txStore.Projects().Create(ctx, p)
							return fmt.Errorf("intentional rollback error")
						})
						atomic.AddInt64(&rollbacksCompleted, 1)
					}

					time.Sleep(time.Duration(r.Intn(3)+1) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Stress test completed: %d writes, %d reads, %d intentional rollbacks across 100 workers",
		atomic.LoadInt64(&writesCompleted), atomic.LoadInt64(&readsCompleted), atomic.LoadInt64(&rollbacksCompleted))

	assert.Greater(t, atomic.LoadInt64(&writesCompleted), int64(10))
	assert.Greater(t, atomic.LoadInt64(&readsCompleted), int64(10))

	// Verify database consistency after stress test
	projects, err := st.Projects().List(context.Background(), rootOrg.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(projects), int(atomic.LoadInt64(&writesCompleted)))
}
