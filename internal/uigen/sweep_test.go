package uigen

import (
	"strings"
	"testing"

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

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
