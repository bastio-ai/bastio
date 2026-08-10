package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps a Redis client with typed get/set operations.
type Cache struct {
	client *redis.Client
}

// New creates a new Redis cache connection.
func New(ctx context.Context, redisURL string) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	slog.Info("connected to redis")
	return &Cache{client: client}, nil
}

// Ping checks the Redis connection.
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close closes the Redis connection.
func (c *Cache) Close() error {
	return c.client.Close()
}

// Client returns the underlying Redis client for direct access when needed.
func (c *Cache) Client() *redis.Client {
	return c.client
}

// Set stores a value with a TTL.
func (c *Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// Get retrieves a value and unmarshals it into dest.
// Returns false if the key does not exist.
func (c *Cache) Get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get key %s: %w", key, err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return false, fmt.Errorf("unmarshal value: %w", err)
	}
	return true, nil
}

// Del deletes one or more keys.
func (c *Cache) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// Incr increments a key and returns the new value. Used for rate limiting.
func (c *Cache) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

// Expire sets a TTL on an existing key.
func (c *Cache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.client.Expire(ctx, key, ttl).Err()
}

type CacheConfig struct {
	Enabled               bool     `json:"enabled"`
	TTLSeconds            int      `json:"ttl_seconds"`
	CacheNondeterministic bool     `json:"cache_nondeterministic"`
	OptOutModels          []string `json:"opt_out_models"`
	OptOutRoutes          []string `json:"opt_out_routes"`
}

// ShouldBypass checks if caching is disabled or if model/path match opt-out exclusions.
func (c *Cache) ShouldBypass(ctx context.Context, model, path string) bool {
	if c == nil {
		return true
	}
	var cfg CacheConfig
	found, err := c.Get(ctx, "cache:config", &cfg)
	if !found || err != nil {
		return false
	}

	if !cfg.Enabled {
		return true
	}

	normModel := strings.ToLower(strings.TrimSpace(model))
	normPath := strings.ToLower(strings.TrimSpace(path))

	for _, opt := range cfg.OptOutModels {
		if normModel != "" && strings.EqualFold(strings.TrimSpace(opt), normModel) {
			return true
		}
	}

	for _, route := range cfg.OptOutRoutes {
		if normPath != "" && strings.EqualFold(strings.TrimSpace(route), normPath) {
			return true
		}
	}

	return false
}
