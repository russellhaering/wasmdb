package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/russellhaering/wasmdb/internal/agent"
	"github.com/russellhaering/wasmdb/internal/agents"
	"github.com/russellhaering/wasmdb/internal/api"
	"github.com/russellhaering/wasmdb/internal/auth"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/config"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/embedding"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/mcpservers"
	"github.com/russellhaering/wasmdb/internal/memory"
	"github.com/russellhaering/wasmdb/internal/skills"
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
		ChatModel:       cfg.ChatModel,
		SubAgentModel:   cfg.SubAgentModel,
	})
	if err != nil {
		slog.Error("failed to create server", "err", err)
		os.Exit(1)
	}

	// Set up the agent scheduler if Anthropic API key is available.
	var scheduler *agents.Scheduler
	if cfg.AnthropicAPIKey != "" {
		// Register the server factory that the scheduler uses to build MCP tool groups.
		agents.SetServerFactory(func(factoryCtx context.Context, schedulerCfg agents.SchedulerConfig) (*mcpx.ServerGroup, func(), error) {
			fnEngine := functions.NewEngine(schedulerCfg.Registry, 0, 0)
			fnStore := functions.NewStore(schedulerCfg.Registry)
			skillStore := skills.NewStore(schedulerCfg.Registry, fnStore, fnEngine)
			memoryStore := memory.NewStore(schedulerCfg.Registry)

			var mcpStore *mcpservers.Store
			if schedulerCfg.MCPServerStore != nil {
				mcpStore = schedulerCfg.MCPServerStore
			}

			servers := mcpx.NewServerGroup()
			tableServer := agent.NewTableServer(schedulerCfg.Registry, fnEngine, fnStore, skillStore, memoryStore, schedulerCfg.SubAgentModel, schedulerCfg.AnthropicAPIKey, mcpStore)
			servers.AddServer("table", tableServer.Server)

			if err := servers.Connect(factoryCtx); err != nil {
				return nil, nil, err
			}

			tableServer.SetServerGroup(servers)

			cleanup := func() {
				servers.Close()
			}
			return servers, cleanup, nil
		})

		scheduler = agents.NewScheduler(srv.AgentStore(), agents.SchedulerConfig{
			Registry:        registry,
			AnthropicAPIKey: cfg.AnthropicAPIKey,
			Model:           cfg.ChatModel,
			SubAgentModel:   cfg.SubAgentModel,
			FnEngine:        functions.NewEngine(registry, 0, 0),
			FnStore:         functions.NewStore(registry),
			MCPServerStore:  mcpservers.NewStore(registry),
		})
		srv.SetAgentScheduler(scheduler)

		// Ensure built-in agents exist (e.g., ui-builder).
		agents.EnsureBuiltinAgents(ctx, srv.AgentStore())

		scheduler.Start(ctx)
		slog.Info("agent scheduler enabled")
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig)
		if scheduler != nil {
			scheduler.Stop()
		}
		cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.Start(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
