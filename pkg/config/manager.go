package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
)

type configManagerImpl struct {
	store    store.Store
	vault    crypto.Vault
	resolver *DAGResolver
}

// NewConfigManager creates a new 4-tier hierarchical ConfigManager.
func NewConfigManager(st store.Store, vault crypto.Vault) ConfigManager {
	return &configManagerImpl{
		store:    st,
		vault:    vault,
		resolver: NewDAGResolver(),
	}
}

// ResolveHierarchy fetches Org -> Project -> Stage -> Service variables,
// applies cascading precedence overrides, decrypts secrets, and expands DAG references.
func (m *configManagerImpl) ResolveHierarchy(
	ctx context.Context,
	orgID, projectID, stageID, serviceID string,
) (*ResolvedEnv, error) {
	rawMap := make(map[string]string)
	secretsMap := make(map[string]bool)

	type tierTarget struct {
		tier       store.ScopeTier
		resourceID string
	}

	targets := []tierTarget{
		{tier: store.TierOrg, resourceID: orgID},
		{tier: store.TierProject, resourceID: projectID},
		{tier: store.TierStage, resourceID: stageID},
		{tier: store.TierService, resourceID: serviceID},
	}

	for _, t := range targets {
		if t.resourceID == "" || m.store == nil {
			continue
		}

		vars, err := m.store.EnvVars().ListByResource(ctx, t.tier, t.resourceID)
		if err != nil {
			return nil, fmt.Errorf("config: failed to fetch env vars for tier %s (%s): %w", t.tier, t.resourceID, err)
		}

		for _, v := range vars {
			decryptedValue := v.ValueEncrypted
			if strings.HasPrefix(v.ValueEncrypted, "v1:") && m.vault != nil {
				decrypted, err := m.vault.DecryptString(ctx, v.ValueEncrypted)
				if err != nil {
					return nil, fmt.Errorf("config: failed to decrypt secret key %q: %w", v.Key, err)
				}
				decryptedValue = decrypted
			}

			// Lower-level tier overrides higher-level tier
			rawMap[v.Key] = decryptedValue
			secretsMap[v.Key] = v.IsSecret
		}
	}

	// Expand DAG variable references
	expanded, err := m.resolver.ResolveDAG(rawMap)
	if err != nil {
		return nil, err
	}

	return &ResolvedEnv{
		Variables: expanded,
		Secrets:   secretsMap,
	}, nil
}

// ExpandVariables performs DAG dependency sorting and cycle detection on raw map.
func (m *configManagerImpl) ExpandVariables(raw map[string]string) (map[string]string, error) {
	return m.resolver.ResolveDAG(raw)
}

// BuildMasker returns a SecretMasker initialized with all secret values in this resolution.
func (m *configManagerImpl) BuildMasker(resolved *ResolvedEnv) SecretMasker {
	if resolved == nil {
		return NewSecretMasker(nil)
	}

	var secrets []string
	for k, isSecret := range resolved.Secrets {
		if isSecret {
			if val, ok := resolved.Variables[k]; ok && val != "" {
				secrets = append(secrets, val)
			}
		}
	}

	return NewSecretMasker(secrets)
}
