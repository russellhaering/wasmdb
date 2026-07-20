package uigen

import (
	"strings"
	"testing"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

func TestSweepCreatesUpdatesSkipsDeletes(t *testing.T) {
	ctx, reg, store, _, gen := newTestGen(t)

	createTable(t, ctx, reg, "orders", ordersSchema())
	postsTbl := createTable(t, ctx, reg, "posts", nil)

	// Fresh sweep: both tables get scaffold pages.
	res, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("initial Sweep: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("expected 2 created, got %v", res.Created)
	}
	for _, name := range []string{"tbl-orders", "tbl-posts"} {
		cfg := waitGet(t, ctx, store, name)
		if cfg.Generator != GeneratorScaffold {
			t.Fatalf("page %q generator = %q, want scaffold", name, cfg.Generator)
		}
	}

	// Claim tbl-orders for the agent and capture its surface. It must never be
	// clobbered by a later sweep.
	agentSurface := `{"components":[{"id":"root","type":"Text","properties":{"value":"agent owned"}}]}`
	if _, err := store.Update(ctx, "tbl-orders", uiconfig.UpdateParams{
		Generator:   strptr("agent"),
		SurfaceJSON: strptr(agentSurface),
	}); err != nil {
		t.Fatalf("claim tbl-orders for agent: %v", err)
	}

	// Change posts data so its scaffold page would regenerate differently.
	putDoc(t, ctx, postsTbl, map[string]any{"title": "Hello", "views": 5})

	res2, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if !contains(res2.Skipped, "tbl-orders") {
		t.Fatalf("expected tbl-orders skipped, got skipped=%v", res2.Skipped)
	}
	if !contains(res2.Updated, "tbl-posts") {
		t.Fatalf("expected tbl-posts updated, got updated=%v", res2.Updated)
	}

	// Agent page is byte-identical (untouched).
	orders := waitGet(t, ctx, store, "tbl-orders")
	if orders.SurfaceJSON != agentSurface || orders.Generator != "agent" {
		t.Fatalf("agent page was clobbered: generator=%q surface=%q", orders.Generator, orders.SurfaceJSON)
	}

	// Scaffold posts page now reflects the new column ("views").
	posts := waitGet(t, ctx, store, "tbl-posts")
	if !strings.Contains(posts.SurfaceJSON, `"views"`) {
		t.Fatalf("regenerated tbl-posts missing inferred column: %s", posts.SurfaceJSON)
	}

	// Drop posts: its scaffold page must be deleted.
	if err := reg.DeleteTable(ctx, "posts"); err != nil {
		t.Fatalf("delete table posts: %v", err)
	}
	res3, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("third Sweep: %v", err)
	}
	if !contains(res3.Deleted, "tbl-posts") {
		t.Fatalf("expected tbl-posts deleted, got deleted=%v", res3.Deleted)
	}
	waitGone(t, ctx, store, "tbl-posts")

	// The agent page for the still-present orders table remains skipped.
	if !contains(res3.Skipped, "tbl-orders") {
		t.Fatalf("expected tbl-orders still skipped, got %v", res3.Skipped)
	}
}

// TestSweepKeepsUserPageForDroppedTable verifies a non-scaffold page is not
// deleted even when its source table disappears.
func TestSweepKeepsUserPageForDroppedTable(t *testing.T) {
	ctx, reg, store, _, gen := newTestGen(t)
	createTable(t, ctx, reg, "orders", ordersSchema())

	if _, err := gen.Sweep(ctx); err != nil {
		t.Fatalf("initial Sweep: %v", err)
	}
	// Convert the scaffold page to a user-owned page.
	if _, err := store.Update(ctx, "tbl-orders", uiconfig.UpdateParams{Generator: strptr("user")}); err != nil {
		t.Fatalf("claim page for user: %v", err)
	}

	if err := reg.DeleteTable(ctx, "orders"); err != nil {
		t.Fatalf("delete table: %v", err)
	}
	res, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep after drop: %v", err)
	}
	if contains(res.Deleted, "tbl-orders") {
		t.Fatalf("user page was deleted for a dropped table: %v", res.Deleted)
	}
	if waitGet(t, ctx, store, "tbl-orders") == nil {
		t.Fatal("user page unexpectedly gone")
	}
}

// TestSweepIdempotentNoUpdates verifies that a second consecutive sweep over an
// unchanged schema/data set reports zero Updated: because generated output is
// deterministic, the regenerated page is byte-identical and the store Update is
// skipped.
func TestSweepIdempotentNoUpdates(t *testing.T) {
	ctx, reg, store, _, gen := newTestGen(t)
	createTable(t, ctx, reg, "orders", ordersSchema())
	postsTbl := createTable(t, ctx, reg, "posts", nil)
	putDoc(t, ctx, postsTbl, map[string]any{"title": "Hello", "views": 5})

	res1, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	if len(res1.Created) != 2 {
		t.Fatalf("expected 2 created on first sweep, got %v", res1.Created)
	}
	// Ensure both pages are visible before the second sweep.
	waitGet(t, ctx, store, "tbl-orders")
	waitGet(t, ctx, store, "tbl-posts")

	res2, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if len(res2.Updated) != 0 {
		t.Fatalf("expected 0 updated on identical re-sweep, got %v", res2.Updated)
	}
	if len(res2.Created) != 0 {
		t.Fatalf("expected 0 created on second sweep, got %v", res2.Created)
	}
	if !contains(res2.Skipped, "tbl-orders") || !contains(res2.Skipped, "tbl-posts") {
		t.Fatalf("expected both pages skipped as unchanged, got skipped=%v", res2.Skipped)
	}
}

// TestSweepDisablesLegacyPages seeds a v1 page document directly into the
// _ui_configs table (bypassing Store.Create, which forces spec_version=2), then
// verifies Sweep disables it (never deletes it) and still scaffolds a v2 page
// for its table.
func TestSweepDisablesLegacyPages(t *testing.T) {
	ctx, reg, store, _, gen := newTestGen(t)
	createTable(t, ctx, reg, "orders", ordersSchema())

	// Seed a legacy (spec_version=1) page for the orders table with a distinct
	// name so it does not collide with the tbl-orders scaffold page.
	uiTbl, err := reg.GetTable(ctx, "_ui_configs")
	if err != nil {
		t.Fatalf("get _ui_configs table: %v", err)
	}
	legacy := &document.Document{Attributes: map[string]any{
		"name":          "legacy-orders",
		"title":         "Legacy Orders Dashboard",
		"source_tables": []any{"orders"},
		"surface_json":  `{"components":[{"id":"root","type":"Text","properties":{"value":"legacy"}}]}`,
		"enabled":       true,
		"spec_version":  1,
		"created_by":    "seed",
		"updated_at":    "2026-01-01T00:00:00Z",
		// generator intentionally absent.
	}}
	if err := uiTbl.PutDocument(ctx, legacy); err != nil {
		t.Fatalf("seed legacy page: %v", err)
	}
	// Also seed a v0 page (spec_version absent entirely) to exercise the "missing"
	// case.
	legacy0 := &document.Document{Attributes: map[string]any{
		"name":         "legacy-orders-v0",
		"title":        "Even older dashboard",
		"surface_json": `{"components":[{"id":"root","type":"Text","properties":{"value":"v0"}}]}`,
		"enabled":      true,
		"created_by":   "seed",
		"updated_at":   "2026-01-01T00:00:00Z",
	}}
	if err := uiTbl.PutDocument(ctx, legacy0); err != nil {
		t.Fatalf("seed legacy v0 page: %v", err)
	}
	// Wait for both to be indexed so Sweep's List sees them.
	waitGet(t, ctx, store, "legacy-orders")
	waitGet(t, ctx, store, "legacy-orders-v0")

	res, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if !contains(res.Disabled, "legacy-orders") || !contains(res.Disabled, "legacy-orders-v0") {
		t.Fatalf("expected both legacy pages disabled, got disabled=%v", res.Disabled)
	}
	if !contains(res.Created, "tbl-orders") {
		t.Fatalf("expected tbl-orders scaffold created, got created=%v", res.Created)
	}

	// Legacy pages still exist but are disabled (not deleted).
	for _, name := range []string{"legacy-orders", "legacy-orders-v0"} {
		cfg := waitGet(t, ctx, store, name)
		if cfg == nil {
			t.Fatalf("legacy page %q was deleted; it should only be disabled", name)
		}
		if cfg.Enabled {
			t.Fatalf("legacy page %q still enabled after sweep", name)
		}
	}

	// The scaffold page for the table is present, enabled, and v2.
	scaffold := waitGet(t, ctx, store, "tbl-orders")
	if scaffold.Generator != GeneratorScaffold || scaffold.SpecVersion != currentSpecVersion || !scaffold.Enabled {
		t.Fatalf("tbl-orders unexpected: generator=%q spec=%d enabled=%v", scaffold.Generator, scaffold.SpecVersion, scaffold.Enabled)
	}

	// A second sweep is a no-op for the already-disabled legacy pages.
	res2, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if len(res2.Disabled) != 0 {
		t.Fatalf("expected no re-disable on second sweep, got %v", res2.Disabled)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
