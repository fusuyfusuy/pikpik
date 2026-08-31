package templates

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalog_CountAndIntegrity(t *testing.T) {
	cat := DefaultCatalog()
	all := cat.ListTemplates("")
	if len(all) < 22 {
		t.Fatalf("expected at least 22 templates in catalog, got %d", len(all))
	}

	expectedIDs := []string{
		// Productivity & DevTools (5)
		"n8n", "vaultwarden", "rabbitmq", "uptime-kuma", "supabase-studio",
		// Databases (8)
		"pocketbase", "meilisearch", "postgres-16", "mysql-8", "redis-7", "mongodb-7", "clickhouse", "mariadb",
		// Storage (1)
		"minio",
		// Analytics (5)
		"plausible", "umami", "grafana", "prometheus", "metabase",
		// CMS (3)
		"directus", "ghost", "wordpress",
	}

	for _, id := range expectedIDs {
		tpl, err := cat.GetTemplate(id)
		if err != nil {
			t.Errorf("expected template '%s' to exist, got error: %v", id, err)
			continue
		}
		if tpl.ID != id {
			t.Errorf("expected ID '%s', got '%s'", id, tpl.ID)
		}
		if tpl.Name == "" {
			t.Errorf("template '%s' missing Name", id)
		}
		if tpl.Category == "" {
			t.Errorf("template '%s' missing Category", id)
		}
		if tpl.Description == "" {
			t.Errorf("template '%s' missing Description", id)
		}
		if tpl.DefaultPort <= 0 {
			t.Errorf("template '%s' invalid DefaultPort: %d", id, tpl.DefaultPort)
		}
		if len(tpl.Services) == 0 {
			t.Errorf("template '%s' has no Services defined", id)
		}
		for _, svc := range tpl.Services {
			if svc.Name == "" {
				t.Errorf("template '%s' has service with empty Name", id)
			}
			if svc.Image == "" {
				t.Errorf("template '%s' has service with empty Image", id)
			}
		}
		for _, v := range tpl.Volumes {
			if v.Name == "" || v.MountPath == "" {
				t.Errorf("template '%s' has invalid volume: %+v", id, v)
			}
		}
		for _, ev := range tpl.EnvVars {
			if ev.Key == "" || ev.Description == "" {
				t.Errorf("template '%s' has invalid env var: %+v", id, ev)
			}
		}
	}
}

func TestCatalog_CategoryFiltering(t *testing.T) {
	cat := DefaultCatalog()

	// 1. Productivity
	prod := cat.ListTemplates("Productivity")
	if len(prod) != 4 {
		t.Errorf("expected 4 Productivity templates, got %d", len(prod))
	}

	// 2. Storage
	storage := cat.ListTemplates("Storage")
	if len(storage) != 1 {
		t.Errorf("expected 1 Storage template, got %d", len(storage))
	}
	if len(storage) > 0 && storage[0].ID != "minio" {
		t.Errorf("expected minio in storage, got %s", storage[0].ID)
	}

	// 3. Analytics
	analytics := cat.ListTemplates("Analytics")
	if len(analytics) != 5 {
		t.Errorf("expected 5 Analytics templates, got %d", len(analytics))
	}

	// 4. CMS
	cms := cat.ListTemplates("CMS")
	if len(cms) != 3 {
		t.Errorf("expected 3 CMS templates, got %d", len(cms))
	}

	// 5. Databases
	databases := cat.ListTemplates("Databases")
	if len(databases) != 8 {
		t.Errorf("expected 8 Databases templates, got %d", len(databases))
	}
	databasesAlias := cat.ListTemplates("database")
	if len(databasesAlias) != 8 {
		t.Errorf("expected alias 'database' to return 8 templates, got %d", len(databasesAlias))
	}

	// 6. Compound category: Productivity & DevTools
	devTools := cat.ListTemplates("Productivity & DevTools")
	if len(devTools) != 5 {
		t.Errorf("expected 5 Productivity & DevTools templates, got %d", len(devTools))
	}

	// 7. Compound category: Analytics & CMS
	analyticsCMS := cat.ListTemplates("Analytics & CMS")
	if len(analyticsCMS) != 8 {
		t.Errorf("expected 8 Analytics & CMS templates, got %d", len(analyticsCMS))
	}

	// 8. All
	allEmpty := cat.ListTemplates("")
	allWildcard := cat.ListTemplates("all")
	if len(allEmpty) != len(allWildcard) || len(allEmpty) < 22 {
		t.Errorf("expected all templates (len >= 22), got empty=%d, all=%d", len(allEmpty), len(allWildcard))
	}
}

func TestCatalog_GetTemplate(t *testing.T) {
	cat := DefaultCatalog()

	// Found
	tpl, err := cat.GetTemplate("pocketbase")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tpl.Name != "PocketBase" {
		t.Errorf("expected PocketBase, got %s", tpl.Name)
	}
	if tpl.DefaultPort != 8090 {
		t.Errorf("expected port 8090, got %d", tpl.DefaultPort)
	}

	// Found case-insensitive
	tplUpper, err := cat.GetTemplate("POCKETBASE")
	if err != nil || tplUpper.ID != "pocketbase" {
		t.Errorf("expected case-insensitive lookup to succeed, got %v", err)
	}

	// Not found
	notFound, err := cat.GetTemplate("unknown-app-xyz")
	if err == nil || notFound != nil {
		t.Errorf("expected error for nonexistent template, got %v", notFound)
	}
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestCatalog_SearchTemplates(t *testing.T) {
	cat := DefaultCatalog()

	// Search by name
	postgresResults := cat.SearchTemplates("", "postgres")
	if len(postgresResults) == 0 {
		t.Fatalf("expected results for search 'postgres', got 0")
	}
	foundPostgres := false
	for _, r := range postgresResults {
		if strings.Contains(strings.ToLower(r.Name), "postgres") || r.ID == "postgres-16" {
			foundPostgres = true
			break
		}
	}
	if !foundPostgres {
		t.Errorf("expected postgres-16 in search results")
	}

	// Search by tag
	sqlResults := cat.SearchTemplates("Databases", "sql")
	if len(sqlResults) < 3 {
		t.Errorf("expected at least 3 SQL database templates, got %d", len(sqlResults))
	}

	// Search with no matches
	noMatches := cat.SearchTemplates("", "nonexistent-query-zzzz")
	if len(noMatches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(noMatches))
	}
}

func TestCatalog_NoLatestTags(t *testing.T) {
	cat := DefaultCatalog()
	templates := cat.ListTemplates("")

	for _, tpl := range templates {
		for _, svc := range tpl.Services {
			if strings.HasSuffix(svc.Image, ":latest") || strings.Contains(svc.Image, ":latest") || strings.HasSuffix(svc.Image, "-latest") {
				t.Errorf("template %s service %s has unpinned floating image tag: %s", tpl.ID, svc.Name, svc.Image)
			}
			// Ensure image has a tag (contains :)
			if !strings.Contains(svc.Image, ":") {
				t.Errorf("template %s service %s image has no tag specified: %s", tpl.ID, svc.Name, svc.Image)
			}
		}
	}
}

