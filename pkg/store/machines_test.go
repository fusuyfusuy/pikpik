package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMachineStore_CRUD(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_machines.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()

	// 1. Create a machine
	now := time.Now().UTC().Truncate(time.Second)
	m := &ManagedMachine{
		ID:            "mch_test1",
		Hostname:      "worker-node-1",
		Role:          "worker",
		PublicIP:      "198.51.100.10",
		PrivateIP:     "10.0.0.10",
		OSKernel:      "Linux 6.8.0-generic",
		CPUArch:       "amd64",
		DockerVersion: "26.1.3",
		AgentVersion:  "1.0.0",
		Status:        "online",
		LastSeen:      &now,
	}

	if err := st.Machines().Create(ctx, m); err != nil {
		t.Fatalf("Create machine failed: %v", err)
	}

	// 2. GetByID
	got, err := st.Machines().GetByID(ctx, "mch_test1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Hostname != "worker-node-1" || got.Role != "worker" || got.Status != "online" {
		t.Errorf("unexpected machine data: %+v", got)
	}

	// 3. GetByHostname
	byHost, err := st.Machines().GetByHostname(ctx, "worker-node-1")
	if err != nil {
		t.Fatalf("GetByHostname failed: %v", err)
	}
	if byHost.ID != "mch_test1" {
		t.Errorf("expected ID mch_test1, got %s", byHost.ID)
	}

	// 4. Update
	got.Status = "degraded"
	got.PublicIP = "198.51.100.11"
	if err := st.Machines().Update(ctx, got); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, err := st.Machines().GetByID(ctx, "mch_test1")
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if updated.Status != "degraded" || updated.PublicIP != "198.51.100.11" {
		t.Errorf("update was not persisted: %+v", updated)
	}

	// 5. UpdateStatus
	newTime := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	if err := st.Machines().UpdateStatus(ctx, "mch_test1", "offline", newTime); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	updatedStatus, err := st.Machines().GetByID(ctx, "mch_test1")
	if err != nil {
		t.Fatalf("GetByID after UpdateStatus failed: %v", err)
	}
	if updatedStatus.Status != "offline" {
		t.Errorf("expected status offline, got %s", updatedStatus.Status)
	}

	// 6. Upsert (existing)
	upsertMachine := &ManagedMachine{
		ID:            "mch_test1",
		Hostname:      "worker-node-1-renamed",
		Role:          "manager",
		Status:        "online",
		DockerVersion: "27.0.1",
		AgentVersion:  "1.1.0",
		LastSeen:      &newTime,
	}
	if err := st.Machines().Upsert(ctx, upsertMachine); err != nil {
		t.Fatalf("Upsert existing failed: %v", err)
	}

	upserted, err := st.Machines().GetByID(ctx, "mch_test1")
	if err != nil {
		t.Fatalf("GetByID after upsert failed: %v", err)
	}
	if upserted.Hostname != "worker-node-1-renamed" || upserted.Role != "manager" || upserted.DockerVersion != "27.0.1" {
		t.Errorf("upsert data mismatch: %+v", upserted)
	}
	// Verify public_ip was preserved from earlier because empty string was passed in upsert
	if upserted.PublicIP != "198.51.100.11" {
		t.Errorf("expected preserved PublicIP 198.51.100.11, got %s", upserted.PublicIP)
	}

	// 7. Upsert (new)
	newMachine := &ManagedMachine{
		ID:       "mch_test2",
		Hostname: "worker-node-2",
		Role:     "worker",
		Status:   "online",
	}
	if err := st.Machines().Upsert(ctx, newMachine); err != nil {
		t.Fatalf("Upsert new failed: %v", err)
	}

	// 8. List
	all, err := st.Machines().List(ctx)
	if err != nil {
		t.Fatalf("List machines failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 machines, got %d", len(all))
	}

	// 9. Delete
	if err := st.Machines().Delete(ctx, "mch_test1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = st.Machines().GetByID(ctx, "mch_test1")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}
