package templates

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalog_CountAndIntegrity(t *testing.T) {
	cat := DefaultCatalog()
	all := cat.ListTemplates("")
	if len(all) < 20 {
		t.Fatalf("expected at least 20 templates in catalog, got %d", len(all))
	}

	expectedIDs := []string{
		// Productivity & DevTools (11)
		"pocketbase", "n8n", "vaultwarden", "meilisearch", "directus",
		"supabase-studio", "minio", "grafana", "prometheus", "metabase", "rabbitmq",
		// Analytics & CMS (4)
		"plausible", "umami", "ghost", "wordpress",
		// Databases (6)
		"postgres-16", "mysql-8", "redis-7", "mongodb-7", "clickhouse", "mariadb",
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
	}
}

func TestCatalog_CategoryFiltering(t *testing.T) {
	cat := DefaultCatalog()

	// 1. Productivity & DevTools
	devTools := cat.ListTemplates("Productivity & DevTools")
	if len(devTools) != 11 {
		t.Errorf("expected 11 Productivity & DevTools templates, got %d", len(devTools))
	}
	devToolsAlias := cat.ListTemplates("devtools")
	if len(devToolsAlias) != 11 {
		t.Errorf("expected alias 'devtools' to return 11 templates, got %d", len(devToolsAlias))
	}

	// 2. Analytics & CMS
	analytics := cat.ListTemplates("Analytics & CMS")
	if len(analytics) != 4 {
		t.Errorf("expected 4 Analytics & CMS templates, got %d", len(analytics))
	}
	analyticsAlias := cat.ListTemplates("cms")
	if len(analyticsAlias) != 4 {
		t.Errorf("expected alias 'cms' to return 4 templates, got %d", len(analyticsAlias))
	}

	// 3. Databases
	databases := cat.ListTemplates("Databases")
	if len(databases) != 6 {
		t.Errorf("expected 6 Databases templates, got %d", len(databases))
	}
	databasesAlias := cat.ListTemplates("database")
	if len(databasesAlias) != 6 {
		t.Errorf("expected alias 'database' to return 6 templates, got %d", len(databasesAlias))
	}

	// 4. All
	allEmpty := cat.ListTemplates("")
	allWildcard := cat.ListTemplates("all")
	if len(allEmpty) != len(allWildcard) || len(allEmpty) < 21 {
		t.Errorf("expected all templates (len >= 21), got empty=%d, all=%d", len(allEmpty), len(allWildcard))
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

