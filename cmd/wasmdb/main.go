package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/russellhaering/wasmdb/internal/api"
	"github.com/russellhaering/wasmdb/internal/auth"
	"github.com/russellhaering/wasmdb/internal/config"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/embedding"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize object store.
	var store objstore.ObjectStore
	if cfg.S3Bucket != "" {
		s3Store, err := objstore.NewS3Store(ctx, objstore.S3Config{
			Bucket:   cfg.S3Bucket,
			Region:   cfg.S3Region,
			Endpoint: cfg.S3Endpoint,
			Prefix:   cfg.S3Prefix,
		})
		if err != nil {
			slog.Error("failed to create S3 store", "err", err)
			os.Exit(1)
		}
		store = s3Store
	} else {
		slog.Warn("no S3 bucket configured, using in-memory store")
		store = objstore.NewMemoryStore()
	}

	// Initialize embedding pipeline (optional).
	var embPipeline *embedding.Pipeline
	if cfg.OpenAIAPIKey != "" {
		embedder := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey: cfg.OpenAIAPIKey,
		})
		embPipeline = embedding.NewPipeline(embedder, 64, 0)
	}

	// Initialize database registry.
	registry := database.NewRegistry(database.RegistryConfig{
		Store:           store,
		Prefix:          cfg.S3Prefix,
		CacheDir:        cfg.CacheDir,
		Embedder:        embPipeline,
		MemTableMaxSize: cfg.MemTableMaxSize,
		L0CompactThresh: cfg.L0CompactThresh,
	})
	defer registry.Close()

	// Ensure system tables exist.
	if err := registry.EnsureSystemTables(ctx, database.SystemTables); err != nil {
		slog.Error("failed to ensure system tables", "err", err)
		os.Exit(1)
	}

	// Seed initial user if configured.
	if cfg.SeedUserEmail != "" && cfg.SeedUserPassword != "" {
		if err := auth.SeedUser(ctx, registry, cfg.SeedUserEmail, cfg.SeedUserPassword); err != nil {
			slog.Error("failed to seed user", "err", err)
			os.Exit(1)
		}
	}

	// Start API server.
	srv, err := api.NewServer(ctx, api.ServerConfig{
		ListenAddr:      cfg.ListenAddr,
		Registry:        registry,
		AnthropicAPIKey: cfg.AnthropicAPIKey,
	})
	if err != nil {
		slog.Error("failed to create server", "err", err)
		os.Exit(1)
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig)
		cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.Start(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
