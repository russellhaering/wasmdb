package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/embedding"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// TableMeta holds persistent metadata about a table.
type TableMeta struct {
	Name      string           `json:"name"`
	Schema    *document.Schema `json:"schema,omitempty"`
	System    bool             `json:"system,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

// SystemTableDef defines a system table to be auto-created at startup.
type SystemTableDef struct {
	Name   string
	Schema *document.Schema
}

// Registry manages multiple tables.
type Registry struct {
	mu     sync.RWMutex
	tables map[string]*Table
	store     objstore.ObjectStore
	prefix    string
	cacheDir  string
	embedder  *embedding.Pipeline

	// LSM config defaults
	memTableMaxSize int64
	l0CompactThresh int

	// OnSchemaChange is called after tables are created, deleted, or schemas are
	// updated. It is called without the registry lock held; implementations must
	// still be fast and must not block (dispatch to a debounced/async worker
	// rather than doing heavy work inline).
	OnSchemaChange func(ctx context.Context)

	// OnWrite is called (nil-guarded) after a successful document write to a
	// non-system table, with the table's name. It fires once per PutDocument and
	// once per PutDocumentsBulk. Like OnSchemaChange it is settable after
	// construction and read without additional locking, so it must be installed
	// before writes begin. Handlers are invoked inline on the write path and must
	// be cheap (dispatch to a debounced/async worker rather than blocking).
	OnWrite func(tableName string)
}

// RegistryConfig configures the registry.
type RegistryConfig struct {
	Store           objstore.ObjectStore
	Prefix          string
	CacheDir        string
	Embedder        *embedding.Pipeline
	MemTableMaxSize int64
	L0CompactThresh int
}

// NewRegistry creates a new table registry.
func NewRegistry(cfg RegistryConfig) *Registry {
	return &Registry{
		tables:          make(map[string]*Table),
		store:           cfg.Store,
		prefix:          cfg.Prefix,
		cacheDir:        cfg.CacheDir,
		embedder:        cfg.Embedder,
		memTableMaxSize: cfg.MemTableMaxSize,
		l0CompactThresh: cfg.L0CompactThresh,
	}
}

func (r *Registry) metaPath(name string) string {
	return fmt.Sprintf("%s/databases/%s/meta.json", r.prefix, name)
}

func (r *Registry) dbPrefix(name string) string {
	return fmt.Sprintf("%s/databases/%s/data", r.prefix, name)
}

// CreateTable creates a new table.
func (r *Registry) CreateTable(ctx context.Context, name string, schema *document.Schema) (*Table, error) {
	db, err := r.createTableLocked(ctx, name, schema)
	if err != nil {
		return nil, err
	}
	// Fire the schema-change callback AFTER releasing r.mu so a callback that
	// re-enters the registry (e.g. GetTable) cannot deadlock.
	r.fireSchemaChange(ctx)
	return db, nil
}

// createTableLocked performs the table creation while holding r.mu.
func (r *Registry) createTableLocked(ctx context.Context, name string, schema *document.Schema) (*Table, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tables[name]; ok {
		return nil, fmt.Errorf("table %q already exists", name)
	}

	// Check if metadata already exists in object store.
	exists, _ := r.store.Exists(ctx, r.metaPath(name))
	if exists {
		return nil, fmt.Errorf("table %q already exists", name)
	}

	meta := TableMeta{
		Name:      name,
		Schema:    schema,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	if err := r.store.Put(ctx, r.metaPath(name), data, true); err != nil {
		return nil, fmt.Errorf("save metadata: %w", err)
	}

	db, err := r.openTable(ctx, name, schema, false)
	if err != nil {
		return nil, err
	}

	r.tables[name] = db
	slog.Info("table created", "name", name)

	return db, nil
}

// fireSchemaChange invokes the OnSchemaChange callback if set. It must be
// called without r.mu held.
func (r *Registry) fireSchemaChange(ctx context.Context) {
	if r.OnSchemaChange != nil {
		r.OnSchemaChange(ctx)
	}
}

// GetTable returns an open table, lazily opening it if needed.
func (r *Registry) GetTable(ctx context.Context, name string) (*Table, error) {
	r.mu.RLock()
	if db, ok := r.tables[name]; ok {
		r.mu.RUnlock()
		return db, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if db, ok := r.tables[name]; ok {
		return db, nil
	}

	// Load metadata.
	metaData, err := r.store.Get(ctx, r.metaPath(name))
	if err != nil {
		return nil, fmt.Errorf("table %q not found", name)
	}

	var meta TableMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("corrupt metadata for %q: %w", name, err)
	}

	db, err := r.openTable(ctx, name, meta.Schema, meta.System)
	if err != nil {
		return nil, err
	}

	r.tables[name] = db
	return db, nil
}

// DeleteTable deletes a table, its metadata, and all stored data.
func (r *Registry) DeleteTable(ctx context.Context, name string) error {
	if err := r.deleteTableLocked(ctx, name); err != nil {
		return err
	}
	// Fire the schema-change callback AFTER releasing r.mu (see CreateTable).
	r.fireSchemaChange(ctx)
	return nil
}

// deleteTableLocked performs the table deletion while holding r.mu.
func (r *Registry) deleteTableLocked(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if db, ok := r.tables[name]; ok {
		db.Close()
		delete(r.tables, name)
	}

	// Delete all objects under the table prefix (LSM data, manifests, WAL).
	dbPrefix := fmt.Sprintf("%s/databases/%s/", r.prefix, name)
	keys, err := r.store.List(ctx, dbPrefix)
	if err != nil {
		return fmt.Errorf("list table objects: %w", err)
	}
	for _, key := range keys {
		if err := r.store.Delete(ctx, key); err != nil {
			slog.Warn("failed to delete object during table cleanup", "key", key, "err", err)
		}
	}

	slog.Info("table deleted", "name", name, "objects_removed", len(keys))

	return nil
}

// ListTables returns metadata for all tables.
func (r *Registry) ListTables(ctx context.Context) ([]TableMeta, error) {
	prefix := fmt.Sprintf("%s/databases/", r.prefix)
	keys, err := r.store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var metas []TableMeta
	for _, key := range keys {
		// Only process meta.json files.
		if !strings.HasSuffix(key, "/meta.json") {
			continue
		}
		data, err := r.store.Get(ctx, key)
		if err != nil {
			continue
		}
		var meta TableMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// UpdateSchema updates a table's schema, rebuilding affected indexes.
func (r *Registry) UpdateSchema(ctx context.Context, name string, schema *document.Schema) error {
	db, err := r.GetTable(ctx, name)
	if err != nil {
		return err
	}

	oldSchema := db.Schema
	if err := db.RebuildIndexes(ctx, oldSchema, schema); err != nil {
		return fmt.Errorf("rebuild indexes: %w", err)
	}

	// Update persisted metadata.
	meta := TableMeta{
		Name:   name,
		Schema: schema,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := r.store.Put(ctx, r.metaPath(name), data, false); err != nil {
		return err
	}

	// UpdateSchema does not hold r.mu here (GetTable released it), so firing is
	// already lock-free; use the shared helper for consistency.
	r.fireSchemaChange(ctx)

	return nil
}

func (r *Registry) openTable(ctx context.Context, name string, schema *document.Schema, system bool) (*Table, error) {
	dbPrefix := r.dbPrefix(name)

	lsmDB, err := lsm.Open(ctx, lsm.DBConfig{
		Store:           r.store,
		Prefix:          dbPrefix,
		MemTableMaxSize: r.memTableMaxSize,
		L0CompactThresh: r.l0CompactThresh,
		CompactInterval: 30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open lsm for %q: %w", name, err)
	}

	tbl, err := NewTable(TableConfig{
		Name:     name,
		System:   system,
		Schema:   schema,
		DB:       lsmDB,
		CacheDir: r.cacheDir,
		Embedder: r.embedder,
	})
	if err != nil {
		return nil, err
	}
	// Back-reference so the table can invoke the registry's OnWrite hook.
	tbl.registry = r
	return tbl, nil
}

// EnsureSystemTables creates any system tables that don't already exist.
// It is idempotent — existing tables are skipped.
func (r *Registry) EnsureSystemTables(ctx context.Context, defs []SystemTableDef) error {
	for _, def := range defs {
		r.mu.Lock()
		if _, ok := r.tables[def.Name]; ok {
			r.mu.Unlock()
			continue
		}

		// Check if metadata already exists in object store.
		exists, _ := r.store.Exists(ctx, r.metaPath(def.Name))
		if exists {
			r.mu.Unlock()
			continue
		}

		meta := TableMeta{
			Name:      def.Name,
			Schema:    def.Schema,
			System:    true,
			CreatedAt: time.Now().UTC(),
		}

		data, err := json.Marshal(meta)
		if err != nil {
			r.mu.Unlock()
			return fmt.Errorf("marshal system table %q meta: %w", def.Name, err)
		}

		if err := r.store.Put(ctx, r.metaPath(def.Name), data, true); err != nil {
			r.mu.Unlock()
			return fmt.Errorf("save system table %q metadata: %w", def.Name, err)
		}

		db, err := r.openTable(ctx, def.Name, def.Schema, true)
		if err != nil {
			r.mu.Unlock()
			return fmt.Errorf("open system table %q: %w", def.Name, err)
		}

		r.tables[def.Name] = db
		r.mu.Unlock()

		slog.Info("system table created", "name", def.Name)

		// r.mu is released above before firing (consistent with CreateTable).
		r.fireSchemaChange(ctx)
	}
	return nil
}

// IsSystemTable returns whether the named table is a system table.
func (r *Registry) IsSystemTable(ctx context.Context, name string) (bool, error) {
	table, err := r.GetTable(ctx, name)
	if err != nil {
		return false, err
	}
	return table.System, nil
}

// Close closes all open tables.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, db := range r.tables {
		if err := db.Close(); err != nil {
			slog.Error("error closing table", "name", name, "err", err)
		}
	}
	r.tables = make(map[string]*Table)
	return nil
}
