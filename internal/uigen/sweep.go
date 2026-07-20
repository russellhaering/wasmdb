package uigen

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

// SweepResult summarizes the page reconciliation performed by Sweep. Each slice
// holds page names, sorted, so the result is deterministic and easy to log.
type SweepResult struct {
	Created  []string
	Updated  []string
	Skipped  []string // pages left untouched because generator != "scaffold"
	Deleted  []string // scaffold pages whose source table no longer exists
	Disabled []string // pages disabled because they predate surface format v2
}

// currentSpecVersion is the surface format version Sweep expects. Pages written
// with any other version (or none) predate the v2 rewrite and are disabled.
const currentSpecVersion = 2

// Sweep reconciles scaffold pages against the current set of user tables:
//
//   - Missing pages for user tables are created with generator="scaffold".
//   - Existing pages with generator=="scaffold" are regenerated (patch Update).
//   - Pages with any other generator ("agent"/"user") are never touched.
//   - Scaffold pages whose source table no longer exists are deleted; pages with
//     other generators are left in place even when their table is gone.
//
// It is safe to call repeatedly (idempotent for a stable schema/data set).
func (g *Generator) Sweep(ctx context.Context) (*SweepResult, error) {
	result := &SweepResult{}

	metas, err := g.registry.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("uigen: list tables: %w", err)
	}
	userTables := map[string]bool{}
	var tableNames []string
	for _, m := range metas {
		if m.System || strings.HasPrefix(m.Name, "_") {
			continue
		}
		userTables[m.Name] = true
		tableNames = append(tableNames, m.Name)
	}
	sort.Strings(tableNames)

	existing, err := g.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("uigen: list pages: %w", err)
	}
	byName := make(map[string]*uiconfig.UIConfig, len(existing))
	for _, cfg := range existing {
		byName[cfg.Name] = cfg
	}

	// Pre-pass: disable any page not on surface format v2. Such pages predate the
	// v2 rewrite and cannot be served by the current validator/render pipeline.
	// They are disabled (enabled=false), never deleted, so their content stays
	// recoverable; scaffold regeneration below restores v2 coverage for their
	// tables. Already-disabled pages are left alone.
	disableCandidates := make([]string, 0, len(byName))
	for name := range byName {
		disableCandidates = append(disableCandidates, name)
	}
	sort.Strings(disableCandidates)
	for _, name := range disableCandidates {
		cfg := byName[name]
		if cfg.SpecVersion == currentSpecVersion || !cfg.Enabled {
			continue
		}
		if err := g.store.SetEnabled(ctx, name, false); err != nil {
			return nil, fmt.Errorf("uigen: disable legacy page %q: %w", name, err)
		}
		cfg.Enabled = false
		result.Disabled = append(result.Disabled, name)
	}

	// Create or regenerate a page per user table.
	for _, tableName := range tableNames {
		pageName := pagePrefix + tableName
		cur, ok := byName[pageName]

		if ok && cur.Generator != GeneratorScaffold {
			result.Skipped = append(result.Skipped, pageName)
			continue
		}

		spec, err := g.GeneratePage(ctx, tableName)
		if err != nil {
			return nil, err
		}

		if !ok {
			if _, err := g.store.Create(ctx, spec.Name, spec.Title, spec.Description,
				spec.SourceTables, spec.SurfaceJSON, spec.ActionsJSON, spec.QueryJS,
				spec.AutoRefreshSeconds, spec.SortOrder, true, GeneratorScaffold, ""); err != nil {
				return nil, fmt.Errorf("uigen: create page %q: %w", pageName, err)
			}
			result.Created = append(result.Created, pageName)
			continue
		}

		// Generated output is deterministic, so most sweeps regenerate a page
		// byte-for-byte identical to the stored one. Skip the write in that case
		// to keep repeated sweeps cheap and quiet (no spurious "updated" logs and
		// no needless LSM churn / updated_at bumps).
		if unchanged(cur, spec) {
			result.Skipped = append(result.Skipped, pageName)
			continue
		}

		if _, err := g.store.Update(ctx, pageName, uiconfig.UpdateParams{
			Title:              &spec.Title,
			Description:        &spec.Description,
			SurfaceJSON:        &spec.SurfaceJSON,
			ActionsJSON:        &spec.ActionsJSON,
			QueryJS:            &spec.QueryJS,
			SourceTables:       &spec.SourceTables,
			AutoRefreshSeconds: &spec.AutoRefreshSeconds,
		}); err != nil {
			return nil, fmt.Errorf("uigen: update page %q: %w", pageName, err)
		}
		result.Updated = append(result.Updated, pageName)
	}

	// Delete scaffold pages whose source table is gone.
	var deletionCandidates []string
	for name := range byName {
		deletionCandidates = append(deletionCandidates, name)
	}
	sort.Strings(deletionCandidates)
	for _, name := range deletionCandidates {
		cfg := byName[name]
		if cfg.Generator != GeneratorScaffold {
			continue
		}
		if userTables[sourceTableOf(cfg)] {
			continue
		}
		if err := g.store.Delete(ctx, name); err != nil {
			return nil, fmt.Errorf("uigen: delete stale page %q: %w", name, err)
		}
		result.Deleted = append(result.Deleted, name)
	}

	return result, nil
}

// unchanged reports whether regenerating cur would produce a byte-identical
// page, comparing only the fields Sweep patches via Update. source_tables and
// auto_refresh_seconds are omitted deliberately: they are constant for a given
// table name, so the load-bearing content is title/description/surface/actions/
// query.
func unchanged(cur *uiconfig.UIConfig, spec *PageSpec) bool {
	return cur.Title == spec.Title &&
		cur.Description == spec.Description &&
		cur.SurfaceJSON == spec.SurfaceJSON &&
		cur.ActionsJSON == spec.ActionsJSON &&
		cur.QueryJS == spec.QueryJS
}

// sourceTableOf resolves the table a scaffold page was generated for, preferring
// its declared source table and falling back to the "tbl-" name convention.
func sourceTableOf(cfg *uiconfig.UIConfig) string {
	if len(cfg.SourceTables) > 0 && cfg.SourceTables[0] != "" {
		return cfg.SourceTables[0]
	}
	return strings.TrimPrefix(cfg.Name, pagePrefix)
}
