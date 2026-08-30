package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
)

func createTestServiceHierarchy(t *testing.T, st store.Store) (string, string) {
	t.Helper()
	ctx := context.Background()

	org := &store.Organization{Name: "Acme Git", Slug: "acme-git-" + store.NewID("tst")}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	proj := &store.Project{OrgID: org.ID, Name: "App", Slug: "app-" + store.NewID("tst")}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	stage := &store.Stage{ProjectID: proj.ID, Name: "Production", Slug: "prod"}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Web App",
		Slug:      "web-app",
		Type:      "app",
		Image:     "nginx:alpine",
	}
	if err := st.Services().Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	return org.ID, svc.ID
}

func TestBuildStore_CRUD(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, serviceID := createTestServiceHierarchy(t, st)

	bld := &store.Build{
		ServiceID:     serviceID,
		RepoURL:       "https://github.com/fusuycorp/pikpik.git",
		Branch:        "main",
		CommitSHA:     "c0ffee1234567890",
		CommitMessage: "feat: git build pipelines",
		Author:        "Fusuy Developer",
		Status:        "queued",
	}

	// 1. Create
	if err := st.Builds().Create(ctx, bld); err != nil {
		t.Fatalf("failed to create build: %v", err)
	}
	if bld.ID == "" {
		t.Fatalf("expected build ID to be generated")
	}

	// 2. GetByID
	fetched, err := st.Builds().GetByID(ctx, bld.ID)
	if err != nil {
		t.Fatalf("failed to get build by id: %v", err)
	}
	if fetched.RepoURL != bld.RepoURL {
		t.Errorf("expected repo_url %s, got %s", bld.RepoURL, fetched.RepoURL)
	}
	if fetched.CommitSHA != "c0ffee1234567890" {
		t.Errorf("expected commit sha %s, got %s", "c0ffee1234567890", fetched.CommitSHA)
	}
	if fetched.Status != "queued" {
		t.Errorf("expected status queued, got %s", fetched.Status)
	}

	// 3. UpdateStatus to building then success
	now := time.Now().UTC()
	err = st.Builds().UpdateStatus(ctx, bld.ID, "success", &now, 12500, "", "pikpik/web-app:c0ffee1234567890")
	if err != nil {
		t.Fatalf("failed to update build status: %v", err)
	}

	updated, err := st.Builds().GetByID(ctx, bld.ID)
	if err != nil {
		t.Fatalf("failed to get updated build: %v", err)
	}
	if updated.Status != "success" {
		t.Errorf("expected status success, got %s", updated.Status)
	}
	if updated.DurationMS != 12500 {
		t.Errorf("expected duration_ms 12500, got %d", updated.DurationMS)
	}
	if updated.ImageTag != "pikpik/web-app:c0ffee1234567890" {
		t.Errorf("expected image_tag pikpik/web-app:c0ffee1234567890, got %s", updated.ImageTag)
	}
	if updated.FinishedAt == nil {
		t.Fatalf("expected finished_at to be populated")
	}

	// 4. ListByService
	list, err := st.Builds().ListByService(ctx, serviceID, 10)
	if err != nil {
		t.Fatalf("failed to list builds: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 build in list, got %d", len(list))
	}

	// 5. Delete
	if err := st.Builds().Delete(ctx, bld.ID); err != nil {
		t.Fatalf("failed to delete build: %v", err)
	}

	// 6. Verify Not Found
	_, err = st.Builds().GetByID(ctx, bld.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after deletion, got %v", err)
	}
}

func TestGitHubInstallationStore_CRUD(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	orgID, _ := createTestServiceHierarchy(t, st)

	inst := &store.GitHubInstallation{
		OrgID:               orgID,
		InstallationID:      987654321,
		AccountName:         "fusuycorp",
		AccountType:         "Organization",
		RepositorySelection: "all",
		Permissions:         `{"contents":"read","metadata":"read"}`,
	}

	// 1. Create
	if err := st.GitHubInstallations().Create(ctx, inst); err != nil {
		t.Fatalf("failed to create github installation: %v", err)
	}
	if inst.ID == "" {
		t.Fatalf("expected installation ID to be generated")
	}

	// 2. GetByID
	byID, err := st.GitHubInstallations().GetByID(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get installation by id: %v", err)
	}
	if byID.InstallationID != 987654321 {
		t.Errorf("expected installation_id 987654321, got %d", byID.InstallationID)
	}
	if byID.AccountName != "fusuycorp" {
		t.Errorf("expected account_name fusuycorp, got %s", byID.AccountName)
	}

	// 3. GetByInstallationID
	byInstID, err := st.GitHubInstallations().GetByInstallationID(ctx, 987654321)
	if err != nil {
		t.Fatalf("failed to get installation by github id: %v", err)
	}
	if byInstID.ID != inst.ID {
		t.Errorf("expected ID %s, got %s", inst.ID, byInstID.ID)
	}

	// 4. ListByOrg
	list, err := st.GitHubInstallations().ListByOrg(ctx, orgID)
	if err != nil {
		t.Fatalf("failed to list installations: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 installation, got %d", len(list))
	}

	// 5. DeleteByInstallationID
	if err := st.GitHubInstallations().DeleteByInstallationID(ctx, 987654321); err != nil {
		t.Fatalf("failed to delete installation by installation_id: %v", err)
	}

	// 6. Verify Not Found
	_, err = st.GitHubInstallations().GetByID(ctx, inst.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound after deletion, got %v", err)
	}
}

func TestBuildStore_TransactionRollback(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	_, serviceID := createTestServiceHierarchy(t, st)

	buildID := ""
	_ = st.WithTx(ctx, func(tx store.Store) error {
		bld := &store.Build{
			ServiceID: serviceID,
			RepoURL:   "https://github.com/fusuycorp/pikpik.git",
			Branch:    "develop",
			CommitSHA: "abcdef0123456789",
		}
		if err := tx.Builds().Create(ctx, bld); err != nil {
			return err
		}
		buildID = bld.ID
		// Force error to rollback transaction
		return errors.New("simulated transaction failure")
	})

	// Build should not exist in database
	_, err := st.Builds().GetByID(ctx, buildID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for rolled-back build, got %v", err)
	}
}
