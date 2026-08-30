package ingress

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/fusuycorp/pikpik/pkg/store"
)

// DomainValidatorFunc allows plain functions to act as DomainValidators.
type DomainValidatorFunc func(ctx context.Context, domain string) (bool, error)

// VerifyDomain calls the underlying function.
func (f DomainValidatorFunc) VerifyDomain(ctx context.Context, domain string) (bool, error) {
	return f(ctx, domain)
}

// MapDomainValidator implements DomainValidator using an in-memory whitelist map.
type MapDomainValidator struct {
	allowed map[string]bool
}

// NewMapDomainValidator creates a validator with the given allowed domains.
func NewMapDomainValidator(domains []string) *MapDomainValidator {
	m := make(map[string]bool, len(domains))
	for _, d := range domains {
		m[strings.ToLower(strings.TrimSpace(d))] = true
	}
	return &MapDomainValidator{allowed: m}
}

// VerifyDomain checks if the domain exists in the allowed set.
func (v *MapDomainValidator) VerifyDomain(ctx context.Context, domain string) (bool, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if v.allowed[d] {
		return true, nil
	}
	return false, nil
}

// StoreDomainValidator implements DomainValidator backed by the persistent store.
type StoreDomainValidator struct {
	db *sql.DB
}

// NewStoreDomainValidator creates a validator backed by SQLite database.
func NewStoreDomainValidator(st store.Store) *StoreDomainValidator {
	return &StoreDomainValidator{
		db: st.DB(),
	}
}

// VerifyDomain checks whether any active service is configured with the given domain.
func (s *StoreDomainValidator) VerifyDomain(ctx context.Context, domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}
	d := strings.ToLower(strings.TrimSpace(domain))

	if s.db == nil {
		return false, nil
	}

	query := `SELECT domain_names FROM services WHERE status NOT IN ('stopped', 'failed')`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return false, fmt.Errorf("ingress: failed to query domain whitelist: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var domainsJSON string
		if err := rows.Scan(&domainsJSON); err != nil {
			continue
		}
		var domainList []string
		if err := json.Unmarshal([]byte(domainsJSON), &domainList); err != nil {
			continue
		}
		for _, registered := range domainList {
			if strings.EqualFold(registered, d) {
				return true, nil
			}
		}
	}

	return false, rows.Err()
}

// NewAskHandler returns an http.HandlerFunc matching Caddy's On-Demand ask protocol.
// Responds with 200 OK if domain is verified and whitelisted, 403 Forbidden otherwise.
func NewAskHandler(validator DomainValidator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain := strings.TrimSpace(r.URL.Query().Get("domain"))
		if domain == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if validator == nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		allowed, err := validator.VerifyDomain(r.Context(), domain)
		if err != nil || !allowed {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}
