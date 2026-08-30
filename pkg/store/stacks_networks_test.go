package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStacksAndNetworksStore_CRUD(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_stacks_networks.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()

	// 1. Create Org & Project
	org := &Organization{Name: "Test Org", Slug: "test-org"}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("Create Org error = %v", err)
	}

	proj := &Project{OrgID: org.ID, Name: "Test Proj", Slug: "test-proj"}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("Create Proj error = %v", err)
	}

	// 2. Test Stacks CRUD
	stack := &Stack{
		ProjectID:   proj.ID,
		Name:        "web-stack",
		ComposeYAML: "version: '3.8'\nservices:\n  web:\n    image: nginx:alpine\n",
		Status:      "stopped",
	}
	if err := st.Stacks().Create(ctx, stack); err != nil {
		t.Fatalf("Create Stack error = %v", err)
	}
	if stack.ID == "" {
		t.Errorf("expected stack ID to be populated")
	}

	gotStack, err := st.Stacks().GetByID(ctx, stack.ID)
	if err != nil {
		t.Fatalf("GetByID Stack error = %v", err)
	}
	if gotStack.Name != "web-stack" || gotStack.ProjectID != proj.ID {
		t.Errorf("unexpected stack: %+v", gotStack)
	}

	byName, err := st.Stacks().GetByName(ctx, proj.ID, "web-stack")
	if err != nil {
		t.Fatalf("GetByName Stack error = %v", err)
	}
	if byName.ID != stack.ID {
		t.Errorf("expected ID %s, got %s", stack.ID, byName.ID)
	}

	gotStack.Status = "running"
	if err := st.Stacks().UpdateStatus(ctx, gotStack.ID, "running"); err != nil {
		t.Fatalf("UpdateStatus error = %v", err)
	}

	projStacks, err := st.Stacks().ListByProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListByProject error = %v", err)
	}
	if len(projStacks) != 1 || projStacks[0].Status != "running" {
		t.Errorf("expected 1 running stack, got %+v", projStacks)
	}

	// 3. Test Managed Networks CRUD
	net := &ManagedNetwork{
		ProjectID:  proj.ID,
		Name:       "pikpik_net_proj_test",
		Driver:     "bridge",
		Scope:      "project",
		IsExternal: false,
	}
	if err := st.Networks().Create(ctx, net); err != nil {
		t.Fatalf("Create Network error = %v", err)
	}
	if net.ID == "" {
		t.Errorf("expected network ID to be populated")
	}

	gotNet, err := st.Networks().GetByID(ctx, net.ID)
	if err != nil {
		t.Fatalf("GetByID Network error = %v", err)
	}
	if gotNet.Name != net.Name || gotNet.Driver != "bridge" {
		t.Errorf("unexpected network: %+v", gotNet)
	}

	nets, err := st.Networks().ListByProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListByProject Networks error = %v", err)
	}
	if len(nets) != 1 {
		t.Errorf("expected 1 network, got %d", len(nets))
	}

	// 4. Test Managed Volumes CRUD
	vol := &ManagedVolume{
		ProjectID: proj.ID,
		Name:      "pikpik_vol_test_data",
		Driver:    "local",
		SizeBytes: 1024 * 1024,
	}
	if err := st.Volumes().CreateManaged(ctx, vol); err != nil {
		t.Fatalf("CreateManaged error = %v", err)
	}
	if vol.ID == "" {
		t.Errorf("expected volume ID to be populated")
	}

	gotVol, err := st.Volumes().GetManagedByID(ctx, vol.ID)
	if err != nil {
		t.Fatalf("GetManagedByID error = %v", err)
	}
	if gotVol.Name != vol.Name || gotVol.SizeBytes != 1024*1024 {
		t.Errorf("unexpected volume: %+v", gotVol)
	}

	vols, err := st.Volumes().ListManagedByProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListManagedByProject error = %v", err)
	}
	if len(vols) != 1 {
		t.Errorf("expected 1 volume, got %d", len(vols))
	}

	// 5. Test Delete
	if err := st.Stacks().Delete(ctx, stack.ID); err != nil {
		t.Fatalf("Delete Stack error = %v", err)
	}
	if err := st.Networks().Delete(ctx, net.ID); err != nil {
		t.Fatalf("Delete Network error = %v", err)
	}
	if err := st.Volumes().DeleteManaged(ctx, vol.ID); err != nil {
		t.Fatalf("Delete Managed Volume error = %v", err)
	}
}
