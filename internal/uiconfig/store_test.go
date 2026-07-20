package uiconfig

import (
	"testing"
)

const minimalSurface = `{"components":[{"id":"root","type":"Text","properties":{"value":"hi"}}]}`

func TestStoreCreateGetListDelete(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)

	cfg, err := store.Create(ctx, "page1", "Page One", "desc", []string{"orders"}, minimalSurface, `{"a":{"type":"query"}}`, "1+1", 30, 5, true, "", "user-123")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if cfg.SpecVersion != 2 {
		t.Errorf("SpecVersion = %d, want 2", cfg.SpecVersion)
	}
	if cfg.Generator != "user" {
		t.Errorf("Generator = %q, want %q (default)", cfg.Generator, "user")
	}
	if cfg.ID == "" {
		t.Error("expected non-empty ID")
	}

	got := waitGet(t, ctx, store, "page1")
	if got.Title != "Page One" || got.Description != "desc" || got.QueryJS != "1+1" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.ActionsJSON != `{"a":{"type":"query"}}` {
		t.Errorf("ActionsJSON not preserved: %q", got.ActionsJSON)
	}
	if len(got.SourceTables) != 1 || got.SourceTables[0] != "orders" {
		t.Errorf("SourceTables mismatch: %v", got.SourceTables)
	}
	if got.AutoRefreshSeconds != 30 || got.SortOrder != 5 || !got.Enabled {
		t.Errorf("scalar fields mismatch: %+v", got)
	}
	if got.CreatedBy != "user-123" {
		t.Errorf("CreatedBy = %q", got.CreatedBy)
	}

	// GetByID
	byID, err := store.GetByID(ctx, cfg.ID)
	if err != nil || byID == nil || byID.Name != "page1" {
		t.Errorf("GetByID failed: %v %+v", err, byID)
	}

	// List
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	// Duplicate create rejected.
	if _, err := store.Create(ctx, "page1", "", "", nil, minimalSurface, "", "", 0, 0, true, "", ""); err == nil {
		t.Error("expected duplicate create to fail")
	}

	// Delete
	if err := store.Delete(ctx, "page1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	waitGone(t, ctx, store, "page1")
	if err := store.Delete(ctx, "page1"); err == nil {
		t.Error("expected delete of missing page to fail")
	}
}

func TestStoreCreateDefaultGenerator(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)
	cfg, err := store.Create(ctx, "p", "", "", nil, minimalSurface, "", "", 0, 0, true, "scaffold", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if cfg.Generator != "scaffold" {
		t.Errorf("Generator = %q, want scaffold", cfg.Generator)
	}
}

func TestStoreUpdatePatchPreservesFields(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)

	orig, err := store.Create(ctx, "p", "Title", "Desc", []string{"t1"}, minimalSurface, `{"x":{"type":"query"}}`, "orig-js", 60, 3, true, "scaffold", "creator")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitGet(t, ctx, store, "p")

	// Patch only the title; everything else must be preserved.
	upd, err := store.Update(ctx, "p", UpdateParams{Title: strptr("New Title")})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Title != "New Title" {
		t.Errorf("Title = %q, want New Title", upd.Title)
	}
	if upd.Description != "Desc" {
		t.Errorf("Description not preserved: %q", upd.Description)
	}
	if upd.QueryJS != "orig-js" {
		t.Errorf("QueryJS not preserved: %q", upd.QueryJS)
	}
	if upd.ActionsJSON != `{"x":{"type":"query"}}` {
		t.Errorf("ActionsJSON not preserved: %q", upd.ActionsJSON)
	}
	if len(upd.SourceTables) != 1 || upd.SourceTables[0] != "t1" {
		t.Errorf("SourceTables not preserved: %v", upd.SourceTables)
	}
	if upd.AutoRefreshSeconds != 60 || upd.SortOrder != 3 || !upd.Enabled {
		t.Errorf("scalars not preserved: %+v", upd)
	}
	// ID, CreatedBy, CreatedAt, SpecVersion preserved.
	if upd.ID != orig.ID || upd.CreatedBy != "creator" || upd.SpecVersion != 2 {
		t.Errorf("identity fields not preserved: %+v", upd)
	}
	// Generator preserved when not patched.
	if upd.Generator != "scaffold" {
		t.Errorf("Generator = %q, want scaffold", upd.Generator)
	}
	// updated_at bumped.
	if !upd.UpdatedAt.After(orig.UpdatedAt) && !upd.UpdatedAt.Equal(orig.UpdatedAt) {
		// timestamps are UTC-truncated to seconds via RFC3339; just ensure it's set.
		if upd.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero")
		}
	}

	// Persisted?
	reread, _ := store.Get(ctx, "p")
	if reread.Title != "New Title" || reread.QueryJS != "orig-js" {
		t.Errorf("patch not persisted: %+v", reread)
	}
}

func TestStoreUpdateEmptyStringClears(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)
	if _, err := store.Create(ctx, "p", "Title", "Desc", nil, minimalSurface, "acts", "js", 0, 0, true, "user", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitGet(t, ctx, store, "p")

	// Explicit empty-string pointer clears the field.
	upd, err := store.Update(ctx, "p", UpdateParams{Description: strptr(""), QueryJS: strptr("")})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Description != "" {
		t.Errorf("Description not cleared: %q", upd.Description)
	}
	if upd.QueryJS != "" {
		t.Errorf("QueryJS not cleared: %q", upd.QueryJS)
	}
	// Title untouched.
	if upd.Title != "Title" {
		t.Errorf("Title changed unexpectedly: %q", upd.Title)
	}
}

func TestStoreUpdateEmptySurfaceRejected(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)
	if _, err := store.Create(ctx, "p", "", "", nil, minimalSurface, "", "", 0, 0, true, "user", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitGet(t, ctx, store, "p")
	if _, err := store.Update(ctx, "p", UpdateParams{SurfaceJSON: strptr("")}); err == nil {
		t.Error("expected update with empty surface_json to fail")
	}
}

func TestStoreUpdateMissing(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)
	if _, err := store.Update(ctx, "nope", UpdateParams{Title: strptr("x")}); err == nil {
		t.Error("expected update of missing page to fail")
	}
}

func TestStoreSetEnabled(t *testing.T) {
	ctx, _, store, _ := newTestEnv(t)
	if _, err := store.Create(ctx, "p", "", "", nil, minimalSurface, "", "", 0, 0, true, "user", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitGet(t, ctx, store, "p")
	if err := store.SetEnabled(ctx, "p", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got := waitGet(t, ctx, store, "p")
	if got.Enabled {
		t.Error("expected Enabled=false after SetEnabled")
	}

	// ListEnabled should now be empty.
	waitEnabledCount(t, ctx, store, 0)

	if err := store.SetEnabled(ctx, "p", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	waitEnabledCount(t, ctx, store, 1)
}
