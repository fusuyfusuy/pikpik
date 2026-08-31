package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
)

func newAdversarialTestDB(t *testing.T) (*store.SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "adversarial.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open adversarial test store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st, dbPath
}

func TestAdversarial_Store_HeavyThreadContention50Goroutines(t *testing.T) {
	st, _ := newAdversarialTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const numWorkers = 50
	var wg sync.WaitGroup
	var opsCompleted int64

	// Pre-create baseline organization
	rootOrg := &store.Organization{
		Name: "Root Chaos Org",
		Slug: "root-chaos-org",
	}
	if err := st.Organizations().Create(context.Background(), rootOrg); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			var localID int64
			for {
				select {
				case <-ctx.Done():
					return
				default:
					localID++
					prjSlug := fmt.Sprintf("prj-%d-%d", workerID, localID)
					// Perform transaction with insert and reads
					txErr := st.WithTx(ctx, func(txStore store.Store) error {
						prj := &store.Project{
							OrgID: rootOrg.ID,
							Name:  "Project " + prjSlug,
							Slug:  prjSlug,
						}
						if pErr := txStore.Projects().Create(ctx, prj); pErr != nil {
							return pErr
						}

						stage := &store.Stage{
							ProjectID: prj.ID,
							Name:      "Production",
							Slug:      "prod-" + prjSlug,
						}
						if stErr := txStore.Stages().Create(ctx, stage); stErr != nil {
							return stErr
						}

						// Create service in project
						svc := &store.Service{
							ProjectID: prj.ID,
							StageID:   stage.ID,
							Name:      "svc-" + prjSlug,
							Slug:      "svc-" + prjSlug,
							Type:      "app",
						}
						if sErr := txStore.Services().Create(ctx, svc); sErr != nil {
							return sErr
						}

						// Add env var
						return txStore.EnvVars().Set(ctx, &store.EnvVar{
							ScopeTier:      store.TierService,
							ResourceID:     svc.ID,
							Key:            "THREAD_ID",
							ValueEncrypted: fmt.Sprintf("v1:enc:%d", workerID),
						})
					})

					if txErr == nil {
						atomic.AddInt64(&opsCompleted, 1)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Completed %d full transactional operations across 50 concurrent goroutines", atomic.LoadInt64(&opsCompleted))

	// Verify database state
	projects, err := st.Projects().List(context.Background(), rootOrg.ID)
	if err != nil {
		t.Fatalf("failed to list projects after contention test: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected projects to be committed")
	}
}

func TestAdversarial_Store_ConcurrentDuplicateSlugConstraintHandling(t *testing.T) {
	st, _ := newAdversarialTestDB(t)
	ctx := context.Background()

	const concurrency = 20
	var wg sync.WaitGroup
	var successCount int64
	var dupCount int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			org := &store.Organization{
				Name: "Duplicate Org Name",
				Slug: "duplicate-target-slug",
			}
			err := st.Organizations().Create(ctx, org)
			if err == nil {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&dupCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt64(&successCount) != 1 {
		t.Fatalf("expected exactly 1 organization creation to succeed, got %d", successCount)
	}
	if atomic.LoadInt64(&dupCount) != concurrency-1 {
		t.Fatalf("expected %d duplicates to be rejected, got %d", concurrency-1, dupCount)
	}
}

func TestAdversarial_Store_WALCheckpointUnderContinuousLoad(t *testing.T) {
	st, _ := newAdversarialTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	org := &store.Organization{
		Name: "WAL Stress Org",
		Slug: "wal-stress-org",
	}
	if err := st.Organizations().Create(context.Background(), org); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var idx int64
			for {
				select {
				case <-ctx.Done():
					return
				default:
					idx++
					_ = st.Projects().Create(ctx, &store.Project{
						OrgID: org.ID,
						Name:  fmt.Sprintf("Wal Prj %d-%d", id, idx),
						Slug:  fmt.Sprintf("wal-prj-%d-%d", id, idx),
					})
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	time.Sleep(200 * time.Millisecond)
	_ = st.WithTx(context.Background(), func(txStore store.Store) error {
		return nil
	})

	wg.Wait()
}

func TestAdversarial_Store_WriteLockTimeoutExpiryAndPoolRecovery(t *testing.T) {
	st, _ := newAdversarialTestDB(t)

	holdTxDone := make(chan struct{})
	txStarted := make(chan struct{})

	go func() {
		_ = st.WithTx(context.Background(), func(tx store.Store) error {
			_ = tx.Organizations().Create(context.Background(), &store.Organization{
				Name: "Locked Org",
				Slug: "locked-org",
			})
			close(txStarted)
			<-holdTxDone
			return nil
		})
	}()

	<-txStarted

	// Goroutine B tries to write with a tight timeout context
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := st.WithTx(ctxTimeout, func(tx store.Store) error {
		return tx.Organizations().Create(ctxTimeout, &store.Organization{
			Name: "Blocked Org",
			Slug: "blocked-org",
		})
	})

	if err == nil {
		t.Fatal("expected Goroutine B to time out, got nil")
	}

	// Release Goroutine A
	close(holdTxDone)
	time.Sleep(50 * time.Millisecond)

	// Goroutine C must succeed
	startC := time.Now()
	errC := st.WithTx(context.Background(), func(tx store.Store) error {
		return tx.Organizations().Create(context.Background(), &store.Organization{
			Name: "Recovered Org",
			Slug: "recovered-org",
		})
	})
	elapsedC := time.Since(startC)

	if errC != nil {
		t.Fatalf("expected Goroutine C to succeed after lock release, got %v", errC)
	}
	if elapsedC > 500*time.Millisecond {
		t.Fatalf("Goroutine C took too long to acquire lock: %v", elapsedC)
	}
}

func TestAdversarial_Store_ExhaustiveCursorClosureAndPoolHealth(t *testing.T) {
	st, _ := newAdversarialTestDB(t)
	ctx := context.Background()

	org := &store.Organization{
		Name: "Cursor Org",
		Slug: "cursor-org",
	}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatal(err)
	}

	prj := &store.Project{
		OrgID: org.ID,
		Name:  "Cursor Prj",
		Slug:  "cursor-prj",
	}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatal(err)
	}

	stage := &store.Stage{
		ProjectID: prj.ID,
		Name:      "Prod",
		Slug:      "prod",
	}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatal(err)
	}

	// Pre-populate 50 services
	for i := 0; i < 50; i++ {
		svc := &store.Service{
			ProjectID: prj.ID,
			StageID:   stage.ID,
			Name:      fmt.Sprintf("cursor-svc-%d", i),
			Slug:      fmt.Sprintf("cursor-svc-%d", i),
			Type:      "app",
		}
		if err := st.Services().Create(ctx, svc); err != nil {
			t.Fatal(err)
		}
	}

	// Run 200 concurrent queries with immediate context cancellation to simulate client disconnection
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cancelCtx, cancel := context.WithTimeout(context.Background(), time.Duration(id%5)*time.Millisecond)
			defer cancel()
			_, _ = st.Services().ListByProject(cancelCtx, prj.ID)
		}(i)
	}

	wg.Wait()

	// Verify database is completely responsive afterwards
	services, err := st.Services().ListByProject(ctx, prj.ID)
	if err != nil {
		t.Fatalf("failed to query services after cancelled cursor runs: %v", err)
	}
	if len(services) != 50 {
		t.Fatalf("expected 50 services, got %d", len(services))
	}
}

func TestAdversarial_Store_DeepHierarchicalCascadeDeleteUnderLoad(t *testing.T) {
	st, _ := newAdversarialTestDB(t)
	ctx := context.Background()

	// Build deep hierarchy:
	// Org (1) -> 5 Projects -> 15 Services -> 30 Deployments + 30 Volumes + 60 EnvVars
	rootOrg := &store.Organization{
		Name: "Cascade Root Org",
		Slug: "cascade-root-org",
	}
	if err := st.Organizations().Create(ctx, rootOrg); err != nil {
		t.Fatal(err)
	}

	var serviceIDs []string
	for p := 0; p < 5; p++ {
		prj := &store.Project{
			OrgID: rootOrg.ID,
			Name:  fmt.Sprintf("Project Cascade %d", p),
			Slug:  fmt.Sprintf("prj-casc-%d", p),
		}
		if err := st.Projects().Create(ctx, prj); err != nil {
			t.Fatal(err)
		}

		stage := &store.Stage{
			ProjectID: prj.ID,
			Name:      "Production",
			Slug:      fmt.Sprintf("prod-%d", p),
		}
		if err := st.Stages().Create(ctx, stage); err != nil {
			t.Fatal(err)
		}

		for s := 0; s < 3; s++ {
			svc := &store.Service{
				ProjectID: prj.ID,
				StageID:   stage.ID,
				Name:      fmt.Sprintf("svc-casc-%d-%d", p, s),
				Slug:      fmt.Sprintf("svc-casc-%d-%d", p, s),
				Type:      "app",
			}
			if err := st.Services().Create(ctx, svc); err != nil {
				t.Fatal(err)
			}
			serviceIDs = append(serviceIDs, svc.ID)

			// Deployments
			for d := 0; d < 2; d++ {
				_ = st.Deployments().Create(ctx, &store.Deployment{
					ServiceID: svc.ID,
					ImageTag:  "alpine:latest",
					Status:    "running",
					CommitSHA: fmt.Sprintf("sha_%d_%d_%d", p, s, d),
				})
			}

			// Volumes
			for v := 0; v < 2; v++ {
				_ = st.Volumes().Create(ctx, &store.Volume{
					ProjectID: prj.ID,
					ServiceID: svc.ID,
					Name:      fmt.Sprintf("vol_%d_%d_%d", p, s, v),
					Slug:      fmt.Sprintf("vol_%d_%d_%d", p, s, v),
					MountPath: "/data",
					Type:      "volume",
				})
			}

			// EnvVars
			for e := 0; e < 4; e++ {
				_ = st.EnvVars().Set(ctx, &store.EnvVar{
					ScopeTier:      store.TierService,
					ResourceID:     svc.ID,
					Key:            fmt.Sprintf("KEY_%d_%d_%d", p, s, e),
					ValueEncrypted: "val",
				})
			}
		}
	}

	// Concurrently read while deleting organization
	stopReaders := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
					targetSvc := serviceIDs[workerID%len(serviceIDs)]
					_, _ = st.EnvVars().ListByResource(ctx, store.TierService, targetSvc)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(r)
	}

	// Delete organization (cascades down everything)
	err := st.Organizations().Delete(ctx, rootOrg.ID)
	if err != nil {
		t.Fatalf("failed to delete root organization: %v", err)
	}

	close(stopReaders)
	wg.Wait()

	// Verify all child objects are purged via ON DELETE CASCADE
	projects, err := st.Projects().List(ctx, rootOrg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after cascade delete, got %d", len(projects))
	}

	for _, sid := range serviceIDs {
		svc, err := st.Services().GetByID(ctx, sid)
		if err == nil || svc != nil {
			t.Fatalf("expected service %s to be deleted by cascade, but found %v", sid, svc)
		}
		deployments, _ := st.Deployments().ListByService(ctx, sid, 100)
		if len(deployments) != 0 {
			t.Fatalf("expected 0 deployments for service %s after cascade, got %d", sid, len(deployments))
		}
		volumes, _ := st.Volumes().ListByService(ctx, sid)
		if len(volumes) != 0 {
			t.Fatalf("expected 0 volumes for service %s after cascade, got %d", sid, len(volumes))
		}
	}
}
