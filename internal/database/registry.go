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

// DatabaseMeta holds persistent metadata about a database.
type DatabaseMeta struct {
	Name      string           `json:"name"`
	Schema    *document.Schema `json:"schema,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

// Registry manages multiple databases.
type Registry struct {
	mu        sync.RWMutex
	databases map[string]*Database
	store     objstore.ObjectStore
	prefix    string
	cacheDir  string
	embedder  *embedding.Pipeline

	// LSM config defaults
	memTableMaxSize int64
	l0CompactThresh int

	// OnSchemaChange is called after databases are created, deleted, or schemas are updated.
	OnSchemaChange func(ctx context.Context)
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

// NewRegistry creates a new database registry.
func NewRegistry(cfg RegistryConfig) *Registry {
	return &Registry{
		databases:       make(map[string]*Database),
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

// CreateDatabase creates a new database.
func (r *Registry) CreateDatabase(ctx context.Context, name string, schema *document.Schema) (*Database, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.databases[name]; ok {
		return nil, fmt.Errorf("database %q already exists", name)
	}

	// Check if metadata already exists in object store.
	exists, _ := r.store.Exists(ctx, r.metaPath(name))
	if exists {
		return nil, fmt.Errorf("database %q already exists", name)
	}

	meta := DatabaseMeta{
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

	db, err := r.openDatabase(ctx, name, schema)
	if err != nil {
		return nil, err
	}

	r.databases[name] = db
	slog.Info("database created", "name", name)

	if r.OnSchemaChange != nil {
		r.OnSchemaChange(ctx)
	}

	return db, nil
}

// GetDatabase returns an open database, lazily opening it if needed.
func (r *Registry) GetDatabase(ctx context.Context, name string) (*Database, error) {
	r.mu.RLock()
	if db, ok := r.databases[name]; ok {
		r.mu.RUnlock()
		return db, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if db, ok := r.databases[name]; ok {
		return db, nil
	}

	// Load metadata.
	metaData, err := r.store.Get(ctx, r.metaPath(name))
	if err != nil {
		return nil, fmt.Errorf("database %q not found", name)
	}

	var meta DatabaseMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("corrupt metadata for %q: %w", name, err)
	}

	db, err := r.openDatabase(ctx, name, meta.Schema)
	if err != nil {
		return nil, err
	}

	r.databases[name] = db
	return db, nil
}

// DeleteDatabase deletes a database, its metadata, and all stored data.
func (r *Registry) DeleteDatabase(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if db, ok := r.databases[name]; ok {
		db.Close()
		delete(r.databases, name)
	}

	// Delete all objects under the database prefix (LSM data, manifests, WAL).
	dbPrefix := fmt.Sprintf("%s/databases/%s/", r.prefix, name)
	keys, err := r.store.List(ctx, dbPrefix)
	if err != nil {
		return fmt.Errorf("list database objects: %w", err)
	}
	for _, key := range keys {
		if err := r.store.Delete(ctx, key); err != nil {
			slog.Warn("failed to delete object during database cleanup", "key", key, "err", err)
		}
	}

	slog.Info("database deleted", "name", name, "objects_removed", len(keys))

	if r.OnSchemaChange != nil {
		r.OnSchemaChange(ctx)
	}

	return nil
}

// ListDatabases returns metadata for all databases.
func (r *Registry) ListDatabases(ctx context.Context) ([]DatabaseMeta, error) {
	prefix := fmt.Sprintf("%s/databases/", r.prefix)
	keys, err := r.store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var metas []DatabaseMeta
	for _, key := range keys {
		// Only process meta.json files.
		if !strings.HasSuffix(key, "/meta.json") {
			continue
		}
		data, err := r.store.Get(ctx, key)
		if err != nil {
			continue
		}
		var meta DatabaseMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// UpdateSchema updates a database's schema.
func (r *Registry) UpdateSchema(ctx context.Context, name string, schema *document.Schema) error {
	db, err := r.GetDatabase(ctx, name)
	if err != nil {
		return err
	}

	db.UpdateSchema(schema)

	// Update persisted metadata.
	meta := DatabaseMeta{
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

	if r.OnSchemaChange != nil {
		r.OnSchemaChange(ctx)
	}

	return nil
}

func (r *Registry) openDatabase(ctx context.Context, name string, schema *document.Schema) (*Database, error) {
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

	return NewDatabase(DatabaseConfig{
		Name:     name,
		Schema:   schema,
		DB:       lsmDB,
		CacheDir: r.cacheDir,
		Embedder: r.embedder,
	})
}

// Close closes all open databases.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, db := range r.databases {
		if err := db.Close(); err != nil {
			slog.Error("error closing database", "name", name, "err", err)
		}
	}
	r.databases = make(map[string]*Database)
	return nil
}
