package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration, loaded from environment variables.
type Config struct {
	ListenAddr string

	// S3 / Object Storage
	S3Bucket   string
	S3Region   string
	S3Endpoint string
	S3Prefix   string

	// Local cache
	CacheDir     string
	CacheMaxSize int64 // bytes

	// LSM tuning
	MemTableMaxSize  int64         // bytes before flush
	L0CompactThresh  int           // number of L0 SSTables before compaction
	WALFlushInterval time.Duration // periodic flush interval

	// Embedding
	OpenAIAPIKey string

	// Chat Agent
	AnthropicAPIKey string

	// Auth
	SeedUserEmail    string
	SeedUserPassword string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		ListenAddr:       envOrDefault("WASMDB_LISTEN_ADDR", ":8080"),
		S3Bucket:         envOrDefault("WASMDB_S3_BUCKET", ""),
		S3Region:         envOrDefault("WASMDB_S3_REGION", "us-east-1"),
		S3Endpoint:       envOrDefault("WASMDB_S3_ENDPOINT", ""),
		S3Prefix:         envOrDefault("WASMDB_S3_PREFIX", "wasmdb"),
		CacheDir:         envOrDefault("WASMDB_CACHE_DIR", "/tmp/wasmdb-cache"),
		CacheMaxSize:     envOrDefaultInt64("WASMDB_CACHE_MAX_SIZE", 1<<30),       // 1GB
		MemTableMaxSize:  envOrDefaultInt64("WASMDB_MEMTABLE_MAX_SIZE", 64<<20),   // 64MB
		L0CompactThresh:  int(envOrDefaultInt64("WASMDB_L0_COMPACT_THRESHOLD", 4)),
		WALFlushInterval: envOrDefaultDuration("WASMDB_WAL_FLUSH_INTERVAL", 1*time.Second),
		OpenAIAPIKey:     envOrDefault("OPENAI_API_KEY", ""),
		AnthropicAPIKey:  envOrDefault("ANTHROPIC_API_KEY", ""),
		SeedUserEmail:    envOrDefault("WASMDB_SEED_USER_EMAIL", ""),
		SeedUserPassword: envOrDefault("WASMDB_SEED_USER_PASSWORD", ""),
	}
}


func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envOrDefaultDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
