package observability

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
)

type CacheSummary struct {
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	TokensInSaved  int64   `json:"tokens_in_saved"`
	TokensOutSaved int64   `json:"tokens_out_saved"`
	CostSavedUSD   float64 `json:"cost_saved_usd"`
}

type CacheSettings struct {
	Enabled               bool         `json:"enabled"`
	TTLSeconds            int          `json:"ttl_seconds"`
	CacheNondeterministic bool         `json:"cache_nondeterministic"`
	OptOutModels          []string     `json:"opt_out_models"`
	OptOutRoutes          []string     `json:"opt_out_routes"`
	Summary               CacheSummary `json:"summary"`
}

type CacheSettingsPatch struct {
	Enabled               *bool     `json:"enabled,omitempty"`
	TTLSeconds            *int      `json:"ttl_seconds,omitempty"`
	CacheNondeterministic *bool     `json:"cache_nondeterministic,omitempty"`
	OptOutModels          []string  `json:"opt_out_models,omitempty"`
	OptOutRoutes          []string  `json:"opt_out_routes,omitempty"`
}

type CacheHandler struct {
	ch    *clickhouse.CH
	redis *cache.Cache
}

func NewCacheHandler(ch *clickhouse.CH, c *cache.Cache) *CacheHandler {
	return &CacheHandler{
		ch:    ch,
		redis: c,
	}
}

func (h *CacheHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	settings := CacheSettings{
		Enabled:               true,
		TTLSeconds:            3600,
		CacheNondeterministic: false,
		OptOutModels:          []string{"gpt-4o-realtime", "claude-3-5-sonnet-live"},
		OptOutRoutes:          []string{"/v1/chat/completions-realtime"},
	}

	if h.redis != nil {
		var stored CacheSettings
		if found, _ := h.redis.Get(ctx, "cache:config", &stored); found {
			settings.Enabled = stored.Enabled
			settings.TTLSeconds = stored.TTLSeconds
			settings.CacheNondeterministic = stored.CacheNondeterministic
			if stored.OptOutModels != nil {
				settings.OptOutModels = stored.OptOutModels
			}
			if stored.OptOutRoutes != nil {
				settings.OptOutRoutes = stored.OptOutRoutes
			}
		}
	}

	var hits, misses int64
	if h.redis != nil {
		client := h.redis.Client()
		if val, err := client.Get(ctx, "cache:hits").Result(); err == nil && val != "" {
			hits, _ = strconv.ParseInt(val, 10, 64)
		}
		if val, err := client.Get(ctx, "cache:misses").Result(); err == nil && val != "" {
			misses, _ = strconv.ParseInt(val, 10, 64)
		}
	}

	if h.ch != nil && h.ch.Conn != nil {
		var chHits, chMisses uint64
		_ = h.ch.Conn.QueryRow(ctx, `
			SELECT
				countIf(trace_name LIKE '%cache hit%') AS hits,
				countIf(trace_name NOT LIKE '%cache hit%') AS misses
			FROM bastio.analytics_request_logs
			WHERE customer_id = toUUID(?)
		`, tenantIDFromCtx(ctx)).Scan(&chHits, &chMisses)

		hits += int64(chHits)
		misses += int64(chMisses)
	}

	settings.Summary = CacheSummary{
		Hits:           hits,
		Misses:         misses,
		TokensInSaved:  hits * 450,
		TokensOutSaved: hits * 210,
		CostSavedUSD:   float64(hits) * 0.0025,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(settings)
}

func (h *CacheHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var patch CacheSettingsPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	current := CacheSettings{
		Enabled:               true,
		TTLSeconds:            3600,
		CacheNondeterministic: false,
		OptOutModels:          []string{"gpt-4o-realtime", "claude-3-5-sonnet-live"},
		OptOutRoutes:          []string{"/v1/chat/completions-realtime"},
	}

	if h.redis != nil {
		_, _ = h.redis.Get(ctx, "cache:config", &current)
	}

	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.TTLSeconds != nil {
		current.TTLSeconds = *patch.TTLSeconds
	}
	if patch.CacheNondeterministic != nil {
		current.CacheNondeterministic = *patch.CacheNondeterministic
	}
	if patch.OptOutModels != nil {
		current.OptOutModels = patch.OptOutModels
	}
	if patch.OptOutRoutes != nil {
		current.OptOutRoutes = patch.OptOutRoutes
	}

	if h.redis != nil {
		_ = h.redis.Set(ctx, "cache:config", current, 0)
	}

	h.GetSettings(w, r)
}

func (h *CacheHandler) FlushCache(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var dropped int64

	if h.redis != nil {
		client := h.redis.Client()
		keys, err := client.Keys(ctx, "devapi:detect:*").Result()
		if err == nil && len(keys) > 0 {
			dropped = int64(len(keys))
			_ = client.Del(ctx, keys...).Err()
		}
		_ = client.Set(ctx, "cache:hits", 0, 0).Err()
		_ = client.Set(ctx, "cache:misses", 0, 0).Err()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"dropped": dropped,
	})
}
