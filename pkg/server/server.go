// Package server hosts the Bastio OSS HTTP server and the extension points
// that downstream callers (managed deployments, custom forks, hosted
// products) use to layer on authentication, billing, and additional
// routes without modifying the OSS code.
//
// Typical OSS usage:
//
//	srv, err := server.New(ctx, cfg,
//	    server.WithDashboard(dashFS),
//	    server.WithOpenAPISpec(spec),
//	)
//	if err != nil { ... }
//	defer srv.Close()
//	srv.Start(ctx)
//
// Typical cloud usage (adds session auth + extra routes):
//
//	srv, err := server.New(ctx, cfg,
//	    server.WithDashboard(cloudDashFS),
//	    server.WithDashboardMiddleware(sess.Middleware),
//	    server.WithMount("/auth", authRouter),
//	    server.WithMount("/billing", billingRouter),
//	)
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/pkg/config"
	"github.com/bastio-ai/bastio/internal/gateway"
	"github.com/bastio-ai/bastio/internal/license"
	"github.com/bastio-ai/bastio/internal/notify"
	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/prompts"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/proxy"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/internal/security/iplist"
	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/bastio-ai/bastio/internal/workspace"
	"github.com/bastio-ai/bastio/pkg/cache"
	"github.com/bastio-ai/bastio/pkg/clickhouse"
	"github.com/bastio-ai/bastio/pkg/database"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/bastio-ai/bastio/pkg/tenant"
)

// Server owns the shared infrastructure (PG, Redis, ClickHouse) and wires the
// OSS HTTP router. It exposes accessors so extension code can reuse the same
// resources, and option hooks so callers can inject middleware and routes.
type Server struct {
	cfg      *config.Config
	db       *database.DB
	redis    *cache.Cache
	ch       *clickhouse.CH
	recorder *observability.Recorder
	river    *river.Client[pgx.Tx]

	// ipList is the optional IP threat-list manager (BASTIO_IPLIST_
	// ENABLED). Nil when disabled — registerV1Routes only mounts the
	// middleware when set, so the default deployment is byte-for-byte
	// unchanged.
	ipList *iplist.Manager

	// workspaceHandler is captured during V1 route registration so the
	// root-level Host-intercept middleware can dispatch custom-domain
	// requests to its HostRoutes() handler. Set by registerV1Routes.
	workspaceHandler *workspace.Handler

	opts options
}

// Option configures a Server at construction time.
type Option func(*options)

type options struct {
	dashboardFS         fs.FS
	openapiSpec         []byte
	docsFS              fs.FS
	rootMiddleware      []func(http.Handler) http.Handler
	dashboardMiddleware []func(http.Handler) http.Handler
	gatewayMiddleware   []func(http.Handler) http.Handler
	mounts              []mountPoint
	apiExtenders         []func(r chi.Router)
	dashboardAPIExtenders []func(r chi.Router)
	workspaceCustomizers []func(WorkspaceCustomizer)
	providersDecorator  func(Provider, Client) Client
	encryption          *encryption.Service
	readTimeout         time.Duration
	writeTimeout        time.Duration
	idleTimeout         time.Duration
	shutdownTimeout     time.Duration
}

type mountPoint struct {
	Prefix  string
	Handler http.Handler
}

// WithDashboard serves the provided filesystem as a single-page application
// on any unmatched route. The filesystem should be rooted at the built dist
// directory (index.html at root).
func WithDashboard(fsys fs.FS) Option {
	return func(o *options) { o.dashboardFS = fsys }
}

// WithOpenAPISpec serves the provided OpenAPI document at GET /openapi.yaml.
func WithOpenAPISpec(spec []byte) Option {
	return func(o *options) { o.openapiSpec = spec }
}

// WithDocs serves a rendered OpenAPI viewer at GET /docs, backed by the
// provided filesystem (expected to contain index.html and its assets).
func WithDocs(docsFS fs.FS) Option {
	return func(o *options) { o.docsFS = docsFS }
}

// WithMiddleware appends middleware to the root router, after the built-in
// RequestID/RealIP/Recoverer/Timeout chain but before any routes are matched.
// Runs on every request, including /health and static dashboard assets.
func WithMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(o *options) { o.rootMiddleware = append(o.rootMiddleware, mw...) }
}

// WithDashboardMiddleware wraps the dashboard management API group
// (/v1/traces, /v1/threats, /v1/analytics, /v1/api-keys, /v1/security,
// /v1/proxies, /v1/provider-keys, /v1/config). Managed deployments use
// this to require a session for dashboard operations while leaving the
// gateway proxy endpoints under API-key auth.
func WithDashboardMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(o *options) { o.dashboardMiddleware = append(o.dashboardMiddleware, mw...) }
}

// WithGatewayMiddleware wraps the gateway proxy endpoint group
// (/v1/chat/completions, /v1/guard/{proxyId}/v1/messages, /v1/traces).
// Runs AFTER the OSS API-key authentication and rate-limit middleware,
// so the request context already carries APIKeyInfo / CustomerID by
// the time the wrapped middleware sees it. Managed deployments use
// this to enforce per-tenant quota and overage caps on gateway traffic
// without modifying OSS behavior.
func WithGatewayMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(o *options) { o.gatewayMiddleware = append(o.gatewayMiddleware, mw...) }
}

// WithMount attaches handler at the given path prefix on the root router.
// Use for cloud-side route groups like /auth or /billing that live outside
// the OSS /v1 API namespace.
func WithMount(prefix string, handler http.Handler) Option {
	return func(o *options) {
		o.mounts = append(o.mounts, mountPoint{Prefix: prefix, Handler: handler})
	}
}

// WithAPIExtension runs fn inside the /v1 route group after the OSS routes
// are registered. Use to add more endpoints under /v1.
func WithAPIExtension(fn func(r chi.Router)) Option {
	return func(o *options) { o.apiExtenders = append(o.apiExtenders, fn) }
}

// WithDashboardAPIExtension installs a callback that registers
// additional /v1 routes INSIDE the dashboard middleware group — same
// place OSS mounts /v1/traces, /v1/sessions, /v1/workspace etc. Cloud
// uses this to mount /v1/governance/dashboard alongside OSS dashboard
// routes so cloud's session auth + RBAC apply uniformly. Differs from
// WithAPIExtension, which runs OUTSIDE that group (for HMAC- /
// token-authenticated endpoints like /v1/governance and /v1/audit).
func WithDashboardAPIExtension(fn func(r chi.Router)) Option {
	return func(o *options) { o.dashboardAPIExtenders = append(o.dashboardAPIExtenders, fn) }
}

// WorkspaceCustomizer is the slim seam exposed to cloud (or any other
// caller of WithWorkspaceCustomize) so callers don't need to import
// `internal/workspace` directly — Go's internal-package visibility
// rule blocks that across modules. Implemented by *workspace.Handler.
type WorkspaceCustomizer interface {
	SetKeyResolver(KeyResolver)
	SetEmbeddingClient(EmbeddingClient)
	// SetBillingGate installs a chi-shaped middleware that runs at the
	// root of the workspace router. Cloud uses this to gate paid-tier
	// surfaces behind an active subscription. OSS leaves it unset.
	SetBillingGate(func(http.Handler) http.Handler)
	// AddCloudRoutes installs a callback that registers cloud-only
	// workspace routes (members, invitations, owner transfer, audit,
	// per-user analytics, slug + branded-domain admin) on the workspace
	// router during route construction. The callback runs AFTER OSS
	// routes register, sharing the billing gate and identity middleware.
	// OSS leaves it unset so the endpoints don't register and 404.
	AddCloudRoutes(func(chi.Router))
	Pool() *pgxpool.Pool
}

// KeyResolver is the alias the workspace handler accepts for resolving
// customer-scoped LLM provider keys. Re-exported here so cloud can
// reference it without importing internal/workspace.
type KeyResolver = workspace.KeyResolver

// EmbeddingClient is the alias the workspace handler accepts for the
// retrieval embedding pipeline. Re-exported alongside KeyResolver.
type EmbeddingClient = workspace.EmbeddingClient

// Context keys re-exported from internal/workspace so cloud's auth
// middleware can inject identity onto a request without importing
// internal packages directly. The handler stack reads these keys via
// the same workspace package so identity threads through end-to-end.
var (
	CustomerIDKey = workspace.CustomerIDKey
	UserIDKey     = workspace.UserIDKey
	UserEmailKey  = workspace.UserEmailKey
	RoleKey       = workspace.RoleKey
)

// Role re-exports for cloud / external callers. The actual hierarchy
// + middleware live in internal/workspace; this layer exposes them
// via pkg/server (the only public extension API) so cloud's auth
// middleware can stash a role + apply RequireRole guards without
// importing internal packages (forbidden across modules).
type Role = workspace.Role

const (
	RoleOwner  = workspace.RoleOwner
	RoleAdmin  = workspace.RoleAdmin
	RoleMember = workspace.RoleMember
	RoleViewer = workspace.RoleViewer
)

// APIKeyCustomerID extracts the authenticated customer's UUID from a
// request context that has passed through the OSS API-key auth
// middleware (the chain installed on /v1/chat/completions et al.).
// Returns (uuid.Nil, false) if no APIKeyInfo is present — middleware
// wrapped via WithGatewayMiddleware should treat that as a pass-through
// so OSS still produces its own 401 unauthenticated response.
func APIKeyCustomerID(ctx context.Context) (uuid.UUID, bool) {
	info, ok := auth.FromContext(ctx)
	if !ok || info == nil {
		return uuid.Nil, false
	}
	return info.CustomerID, true
}

// RequireRole gates a route to callers whose stashed role is at
// least `min`. 403s otherwise. See internal/workspace/rbac.go.
func RequireRole(min Role) func(http.Handler) http.Handler {
	return workspace.RequireRole(min)
}

// RoleFromCtx pulls the stashed role for the current request, or
// RoleViewer if no middleware has set one. Re-exported so cloud's
// /whoami handler can read the auth-middleware-stamped role without
// importing the internal workspace package.
func RoleFromCtx(ctx context.Context) Role {
	return workspace.RoleFromCtx(ctx)
}

// Workspace types re-exported for bastio-cloud's cloud-only workspace
// handlers (mounted via WorkspaceCustomizer.AddCloudRoutes). Cloud is
// blocked by Go's internal-package rule from importing
// internal/workspace directly; these aliases keep the OSS workspace
// package the single source of truth while exposing the surface cloud
// actually needs.
type (
	Store       = workspace.Store
	AuditEntry  = workspace.AuditEntry
	AuditTarget = workspace.AuditTarget
	Invitation  = workspace.Invitation
	TXTResolver = workspace.TXTResolver
)

// NewStore constructs a workspace store against the given pgxpool.
// Cloud passes its own pool (same DATABASE_URL as the OSS server's
// pool — Postgres handles the two-pool case fine).
func NewStore(pool *pgxpool.Pool) *Store { return workspace.NewStore(pool) }

// Workspace error sentinels re-exported so cloud handlers can
// errors.Is(...) against the same values OSS code returns. Aliasing
// via var keeps the identity stable.
var (
	ErrNotFound                = workspace.ErrNotFound
	ErrSlugTaken               = workspace.ErrSlugTaken
	ErrDomainTaken             = workspace.ErrDomainTaken
	ErrInvitationExpired       = workspace.ErrInvitationExpired
	ErrInvitationRevoked       = workspace.ErrInvitationRevoked
	ErrInvitationConsumed      = workspace.ErrInvitationConsumed
	ErrInvitationEmailMismatch = workspace.ErrInvitationEmailMismatch
	ErrSeatLimitReached        = workspace.ErrSeatLimitReached
)

// WithWorkspaceCustomize runs fn against the workspace handler after
// it's been constructed but before route registration. Cloud uses this
// to wire a customer-key resolver, a per-customer embedding client,
// and any other deployment-specific behavior — without forking the
// OSS server.
func WithWorkspaceCustomize(fn func(WorkspaceCustomizer)) Option {
	return func(o *options) { o.workspaceCustomizers = append(o.workspaceCustomizers, fn) }
}

// WithProvidersDecorator installs a generic wrapping function applied to
// every LLM provider Client the OSS gateway and workspace pull from the
// internal registry. fn is called with (provider, raw client) and
// returns the wrapped client.
//
// Use cases are intentionally open-ended: response caching, retry
// shims, per-customer telemetry, traffic shadowing. OSS callers don't
// need this — leaving it unset is the OSS default and clients are
// returned as-is. Cloud uses it to layer cross-cutting behaviour
// (e.g. LLM response caching) without OSS having to know the concept
// exists.
//
// Set once at startup. The decorator runs on every Registry.Get; keep
// it allocation-light. Returning the original client unchanged is fine
// for providers you don't want to wrap.
func WithProvidersDecorator(fn func(Provider, Client) Client) Option {
	return func(o *options) { o.providersDecorator = fn }
}

// WithEncryption wires the encryption service used to envelope provider
// keys at storage time. Without it, the provider-key Create handler
// stores plaintext-in-JSON (legacy/OSS-standalone path); the cloud's
// KeyResolver still tolerates that shape on read for back-compat, but
// new keys created via the cloud-served dashboard should be encrypted.
// Cloud passes the same encryption.Service its KeyResolver uses, so
// the same master key encrypts and decrypts.
func WithEncryption(s *encryption.Service) Option {
	return func(o *options) { o.encryption = s }
}

// WithTimeouts overrides HTTP server read/write/idle timeouts.
func WithTimeouts(read, write, idle time.Duration) Option {
	return func(o *options) {
		o.readTimeout = read
		o.writeTimeout = write
		o.idleTimeout = idle
	}
}

// WithShutdownTimeout overrides how long Start waits for in-flight requests
// to complete during graceful shutdown.
func WithShutdownTimeout(d time.Duration) Option {
	return func(o *options) { o.shutdownTimeout = d }
}

// New opens PostgreSQL, Redis, and ClickHouse connections, runs any pending
// migrations, and returns a Server ready to Start. Callers must defer Close.
func New(ctx context.Context, cfg *config.Config, opts ...Option) (*Server, error) {
	o := options{
		readTimeout:     30 * time.Second,
		writeTimeout:    90 * time.Second,
		idleTimeout:     120 * time.Second,
		shutdownTimeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&o)
	}

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	redisClient, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	ch, err := clickhouse.New(ctx, cfg.ClickHouseURL)
	if err != nil {
		db.Close()
		_ = redisClient.Close()
		return nil, fmt.Errorf("connect clickhouse: %w", err)
	}

	if err := db.Migrate(ctx); err != nil {
		_ = ch.Close()
		_ = redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("run postgres migrations: %w", err)
	}
	if err := ch.Migrate(ctx); err != nil {
		_ = ch.Close()
		_ = redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("run clickhouse migrations: %w", err)
	}

	recorder := observability.NewRecorder(ch)
	recorder.Start(ctx)

	// Run River's internal migrations (idempotent) before constructing
	// the client. cmd/worker and cmd/ingest do this in pkg/queue/queue.go
	// — but cmd/server has its own minimal River client (insert-only,
	// no workers) and previously assumed the tables already existed.
	// That broke deployments where the server starts first / alone:
	// every Knowledge Base ingest enqueue 500'd with
	// `relation "river_job" does not exist`. Migrating here makes the
	// server self-sufficient.
	migrator, err := rivermigrate.New(riverpgxv5.New(db.Pool), nil)
	if err != nil {
		_ = ch.Close()
		_ = redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("create river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		_ = ch.Close()
		_ = redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("run river migrations: %w", err)
	}

	// Insert-only River client. The server enqueues durable jobs (e.g.,
	// governance webhook delivery); cmd/worker (or all-in-one mode) picks
	// them up and processes them with the appropriate registered workers.
	// Failure here is fatal now — a broken queue means broken Knowledge
	// Base ingest, governance webhook retries, and any other River-backed
	// flow. The previous silent-warning swallow caused the relation-
	// missing crash to surface only at runtime.
	rc, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{})
	if err != nil {
		_ = ch.Close()
		_ = redisClient.Close()
		db.Close()
		return nil, fmt.Errorf("create river client: %w", err)
	}

	// Optional IP threat-list manager (FireHOL level1 + Tor exit
	// nodes). Constructed last because a feed problem must never take
	// down core wiring — refresh failures only log, and Lookup returns
	// unlisted verdicts until the first successful fetch. Start kicks
	// off an immediate refresh plus the jittered background loop;
	// Close stops it.
	var ipListMgr *iplist.Manager
	if cfg.IPListEnabled {
		ipListMgr = iplist.NewManager(
			[]iplist.Provider{
				iplist.NewFireHOL(cfg.IPListFireHOLURL, nil),
				iplist.NewTor(cfg.IPListTorURL, nil),
			},
			iplist.WithRefreshInterval(cfg.IPListRefresh),
		)
		ipListMgr.Start(ctx)
		slog.Info("iplist enabled", "block", cfg.IPListBlock, "refresh", cfg.IPListRefresh)
	}

	return &Server{cfg: cfg, db: db, redis: redisClient, ch: ch, recorder: recorder, river: rc, ipList: ipListMgr, opts: o}, nil
}

// DB returns the shared PostgreSQL wrapper.
func (s *Server) DB() *database.DB { return s.db }

// Redis returns the shared Redis client.
func (s *Server) Redis() *cache.Cache { return s.redis }

// ClickHouse returns the shared ClickHouse client.
func (s *Server) ClickHouse() *clickhouse.CH { return s.ch }

// Config returns the loaded configuration.
func (s *Server) Config() *config.Config { return s.cfg }

// Close drains observability buffers then releases all backing connections.
func (s *Server) Close() error {
	var errs []error
	if s.ipList != nil {
		s.ipList.Stop()
	}
	if s.recorder != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), s.opts.shutdownTimeout)
		if err := s.recorder.Close(drainCtx); err != nil {
			errs = append(errs, fmt.Errorf("drain recorder: %w", err))
		}
		cancel()
	}
	if err := s.ch.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close clickhouse: %w", err))
	}
	if err := s.redis.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close redis: %w", err))
	}
	s.db.Close()
	return errors.Join(errs...)
}

// Start builds the router, begins listening, and blocks until ctx is
// cancelled or SIGINT/SIGTERM is received.
func (s *Server) Start(ctx context.Context) error {
	handler := s.Handler()

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  s.opts.readTimeout,
		WriteTimeout: s.opts.writeTimeout,
		IdleTimeout:  s.opts.idleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case sig := <-signals:
		slog.Info("shutdown signal received", "signal", sig.String())
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.shutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("stopped")
	return nil
}

// Handler builds and returns the complete HTTP handler. Exposed primarily
// for tests; Start uses it internally.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(echoRequestIDMiddleware)
	if s.cfg.MaxRequestBodyBytes > 0 {
		r.Use(maxBodyBytesMiddleware(s.cfg.MaxRequestBodyBytes))
	}
	if len(s.cfg.CORSOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.cfg.CORSOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "X-End-User-Id"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}
	r.Use(metrics.Middleware)
	// OSS injects the single-tenant customer; cloud overrides this by
	// mounting its own tenant middleware via WithMiddleware.
	r.Use(tenant.OSSMiddleware)
	// OSS default workspace role: owner. OSS is single-tenant — there's
	// no real authorization boundary, the local user controls
	// everything. Cloud's auth middleware (mounted via WithMiddleware
	// below) overrides this with the actual workspace_members.role
	// lookup so RBAC enforces in multi-tenant deployments.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := workspace.WithRole(r.Context(), workspace.RoleOwner)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	for _, mw := range s.opts.rootMiddleware {
		r.Use(mw)
	}

	s.registerInfraRoutes(r)
	r.Handle("/metrics", promhttp.Handler())
	s.registerMounts(r)
	s.registerV1Routes(r)
	s.registerDashboard(r)

	// Branded-chat custom-domain interception. Wrapping the chi router
	// with the host middleware (rather than installing via r.Use) means
	// custom-domain visitors hit the branded handler BEFORE chi tries
	// to match `/` against the SPA. Skips when no workspace handler
	// or no platform hosts are configured.
	var handler http.Handler = r
	if s.workspaceHandler != nil && len(s.cfg.PlatformHosts) > 0 {
		handler = s.workspaceHandler.HostInterceptMiddleware(s.cfg.PlatformHosts)(handler)
	}
	return handler
}

// echoRequestIDMiddleware copies the chi RequestID into the response so
// clients and support can correlate logs with individual requests.
func echoRequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodyBytesMiddleware caps the request body to n bytes. Reads beyond the
// cap return 413 to the client rather than allowing unbounded buffer growth.
func maxBodyBytesMiddleware(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) registerInfraRoutes(r chi.Router) {
	r.Get("/health", s.healthHandler)
	r.Get("/ready", s.readyHandler)
	if s.opts.openapiSpec != nil {
		spec := s.opts.openapiSpec
		r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(spec)
		})
	}
	if s.opts.docsFS != nil {
		docsHandler := http.StripPrefix("/docs", http.FileServerFS(s.opts.docsFS))
		r.Handle("/docs", docsHandler)
		r.Handle("/docs/*", docsHandler)
	}
}

func (s *Server) registerMounts(r chi.Router) {
	for _, m := range s.opts.mounts {
		r.Mount(m.Prefix, m.Handler)
	}
}

func (s *Server) registerV1Routes(r chi.Router) {
	authSvc := auth.NewService(s.db.Pool, s.redis)
	proxySvc := proxy.NewService(s.db.Pool, s.opts.encryption)

	// Build the engine + profile lookup via the shared constructor
	// so every Bastio surface (gateway, workspace, worker, detect
	// SDK) runs the same detector list. See BuildSecurityEngine.
	secEngine, profileLookup := BuildSecurityEngine(context.Background(), s.db.Pool, s.redis)

	providerRegistry := providers.NewRegistry()
	providerRegistry.Register(providers.NewOpenAIClient())
	providerRegistry.Register(providers.NewAnthropicClient())
	providerRegistry.Register(providers.NewBedrockClient())
	providerRegistry.Register(providers.NewOllamaClient())
	if s.opts.providersDecorator != nil {
		providerRegistry.Decorate(s.opts.providersDecorator)
	}

	gw := gateway.NewHandler(providerRegistry, proxySvc, secEngine, profileLookup, s.recorder, string(s.cfg.SecurityMode))
	rateLimiter := gateway.NewRateLimiter(s.redis)

	// Overlay loader is constructed below; wire it into the gateway too
	// so LLM-proxy traffic honours tenant policy overlays. Must be set
	// after overlayLoader is constructed.

	// Tenant policy overlay — additive versioned customization layered
	// on top of the core profile path. Nil-safe: if DB or cache aren't
	// configured, the loader returns (nil, zero, nil) and DetectHandler
	// behaves as if no overlay were installed.
	overlayStore := overlay.NewStore(s.db.Pool)
	overlayLoader := overlay.NewLoader(s.db.Pool, s.redis)
	overlayHandler := overlay.NewHandler(overlayStore, overlayLoader)
	overlayHandler.SetWarningAnalyzer(security.NewOverlayWarningAnalyzer())
	overlayHandler.SetPreviewRunner(security.NewOverlayPreviewRunner(secEngine, profileLookup, overlayLoader))
	gw.SetOverlayLoader(overlayLoader)

	obsHandler := observability.NewHandler(s.ch)
	promptHandler := prompts.New(s.db.Pool, s.ch)
	// Security gate on saved templates — content lands at the head
	// of every chat that loads the template, same attack surface as
	// the workspace assistant system_prompt path. Both setters share
	// the same engine + profile lookup as the gateway.
	promptHandler.SetSecurityEngine(secEngine)
	promptHandler.SetSecurityProfiles(profileLookup)
	apiKeyHandler := auth.NewAPIKeyHandler(s.db.Pool)
	secProfileHandler := security.NewProfileHandler(s.db.Pool)
	detectHandler := security.NewDetectHandler(secEngine, profileLookup, s.db.Pool)
	detectHandler.SetCache(s.redis)
	detectHandler.SetOverlayLoader(overlayLoader)
	detectHandler.SetOverlayStore(overlayStore)
	detectHandler.SetTraceListener(func(ev security.TraceEvent) {
		s.recorder.RecordTrace(&observability.TraceRecord{
			ID:             ev.ID,
			CustomerID:     ev.CustomerID,
			ProxyID:        ev.ProxyID,
			Method:         ev.Method,
			Path:           ev.Path,
			Provider:       ev.Provider,
			Model:          ev.Model,
			StartedAt:      ev.StartedAt,
			CompletedAt:    ev.CompletedAt,
			DurationMs:     ev.DurationMs,
			Status:         ev.Status,
			HTTPStatus:     ev.HTTPStatus,
			ThreatDetected: ev.ThreatDetected,
			ThreatTypes:    ev.ThreatTypes,
			ThreatScore:    ev.ThreatScore,
			SecurityAction: ev.SecurityAction,
			RequestBody:    ev.RequestBody,
			ResponseBody:   ev.ResponseBody,
			TraceName:      ev.TraceName,
		})

		for _, ft := range ev.FiredThreats {
			s.recorder.RecordThreatEvent(&observability.ThreatEvent{
				ID:             uuid.New(),
				TraceID:        ev.ID,
				CustomerID:     ev.CustomerID,
				ProxyID:        ev.ProxyID,
				ThreatType:     ft.DetectorName,
				ThreatSubtype:  "detection.fired",
				Severity:       ft.Severity,
				Score:          ft.Score,
				Action:         ev.SecurityAction,
				DetectorName:   ft.DetectorName,
				MatchedPattern: ft.MatchedPattern,
				MatchedContent: ft.MatchedContent,
				Confidence:     0.9,
				DetectedAt:     time.Now().UTC(),
			})
		}
	})
	playgroundHandler := security.NewPlaygroundHandler(s.db.Pool)
	providerKeyHandler := proxy.NewProviderKeyHandler(s.db.Pool)
	if s.opts.encryption != nil {
		providerKeyHandler.SetEncryptionService(s.opts.encryption)
	}
	configHandler := config.NewHandler(s.cfg)

	// Governance + audit handlers (Shadow AI extension API, SCIM, MDM
	// bundles, anonymous audit lead-gen) live in bastio-cloud. Cloud
	// constructs them after server.New using the Server accessors
	// (DB / ClickHouse / Redis / River) and mounts via the chi router.
	// OSS deployments don't ship those features.

	// Bastio Workspace — multi-model chat product. Reuses the same provider
	// registry as the gateway so workspace and gateway share connection
	// pools, retries, and rate-limit accounting. The KeyResolver is nil in
	// OSS — falls back to environment variables; managed deployments inject
	// a resolver that walks the customer's provider_keys.
	workspaceStore := workspace.NewStore(s.db.Pool)
	// Default OSS KeyResolver: reads provider keys from the same
	// proxy_provider_keys table the dashboard's Provider Keys page
	// writes to, so a self-hoster who configures their OpenAI key in
	// the dashboard sees workspace chat use it without also needing
	// OPENAI_API_KEY in env. Cloud's WithWorkspaceCustomize callback
	// overrides this via SetKeyResolver with a per-customer
	// implementation that handles billing-mode (BYO vs platform keys).
	workspaceHandler := workspace.NewHandler(workspaceStore, providerRegistry, &ossTenantKeyResolver{proxies: proxySvc})
	// Local-disk blob store for uploaded knowledge files. Cloud injects
	// an S3-backed BlobStore via the same setter when it lands.
	if s.cfg.DataDir != "" {
		workspaceHandler.SetBlobStore(workspace.NewLocalBlobStore(s.cfg.DataDir))
	}
	// Hand workspace the same River client governance uses — uploads
	// enqueue ingestion jobs picked up by cmd/worker. Without a client,
	// the upload endpoint runs the worker inline (synchronous).
	if s.river != nil {
		workspaceHandler.SetRiverClient(s.river)
	}
	// OSS dev embedder: lazy OpenAI client when an API key is in env.
	// Cloud will replace with a per-customer key resolver via the
	// same setter.
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		workspaceHandler.SetEmbeddingClient(workspace.NewOpenAIEmbeddingClient(key))
	}
	// Workspace invitation flow (email + public + invite base URLs)
	// moved to bastio-cloud/internal/workspace alongside the member
	// management handlers — see Service.WithEmailer / WithPublicBaseURL
	// / WithInviteBaseURL. OSS no longer needs to plumb those through.

	// Same security engine + profile lookup + observability recorder
	// the gateway uses, so workspace chat runs through the identical
	// detector pipeline and lands in the same threat / trace catalog.
	// Without these, workspace bypasses security entirely (the
	// pre-pipeline behavior). The handler's send paths nil-check
	// each, so partial wiring is safe in tests.
	workspaceHandler.SetSecurityEngine(secEngine)
	workspaceHandler.SetSecurityProfiles(profileLookup)
	if s.recorder != nil {
		workspaceHandler.SetObservabilityRecorder(s.recorder)
	}
	// Cloud (or any caller using WithWorkspaceCustomize) plugs in
	// per-deployment behavior here — typically a KeyResolver that
	// walks the customer's provider_keys, and an embedding client
	// keyed off the same. Customizers run last so they can override
	// the OSS env-var defaults.
	for _, fn := range s.opts.workspaceCustomizers {
		fn(workspaceHandler)
	}

	r.Route("/v1", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"name": "bastio", "version": "0.1.0"})
		})

		// Dashboard management API. In OSS this is unauthenticated; cloud
		// layers a session middleware here via WithDashboardMiddleware.
		r.Group(func(r chi.Router) {
			for _, mw := range s.opts.dashboardMiddleware {
				r.Use(mw)
			}
			r.Get("/traces", obsHandler.ListTraces)
			r.Get("/traces/{id}", obsHandler.GetTrace)
			r.Get("/traces/{id}/threats", obsHandler.ListTraceThreats)
			r.Get("/traces/{id}/scores", obsHandler.ListScores)
			r.Post("/traces/{id}/scores", obsHandler.CreateScore)
			r.Get("/sessions", obsHandler.ListSessions)
			r.Get("/sessions/{id}", obsHandler.GetSession)
			r.Get("/users", obsHandler.UserAnalytics)
			r.Get("/users/{id}", obsHandler.GetUser)
			r.Get("/threats", obsHandler.ListThreats)
			r.Get("/threats/{id}", obsHandler.GetThreat)
			r.Get("/analytics/overview", obsHandler.AnalyticsOverview)
			r.Get("/analytics/users", obsHandler.UserAnalytics)
			r.Mount("/api-keys", apiKeyHandler.Routes())
			r.Mount("/security", secProfileHandler.Routes())
			r.Mount("/overlays", overlayHandler.Routes())
			r.Mount("/overlay-templates", overlayHandler.TemplatesRoutes())
			r.Mount("/detect", detectHandler.Routes())
			r.Mount("/playground", playgroundHandler.Routes())
			r.Mount("/proxies", proxySvc.Routes(providerKeyHandler))
			r.Mount("/provider-keys", providerKeyHandler.Routes())
			r.Mount("/prompts", promptHandler.Routes())
			piiHandler := detection.NewPIIMaskHandler()
			piiHandler.SetCache(s.redis)
			r.Mount("/pii", piiHandler.Routes())

			agentActionHandler := security.NewAgentActionHandler()
			agentActionHandler.SetCache(s.redis)
			r.Mount("/guardrails", agentActionHandler.Routes())
			r.Mount("/webhooks", notify.NewHandler(notify.NewDispatcher()).Routes())
			r.Mount("/license", license.NewService().Routes())
			cacheHandler := observability.NewCacheHandler(s.ch, s.redis)
			r.Get("/dashboard/cache-settings", cacheHandler.GetSettings)
			r.Put("/dashboard/cache-settings", cacheHandler.UpdateSettings)
			r.Delete("/dashboard/cache", cacheHandler.FlushCache)
			r.Get("/cache/stats", cacheHandler.GetSettings)
			r.Get("/config", configHandler.GetConfig)

			// Cloud-only dashboard route registrations (e.g. /v1/governance/
			// dashboard). Run inside this auth group so cloud's session
			// middleware gates them; OSS leaves the slice empty.
			for _, fn := range s.opts.dashboardAPIExtenders {
				fn(r)
			}

			// Workspace dashboard API. Behind the dashboard auth group so
			// cloud's session middleware gates access; OSS leaves it open
			// (single-tenant). Public branded chat mounts separately
			// outside /v1 (see registerWorkspacePublicRoutes below).
			r.Mount("/workspace", workspaceHandler.Routes())
		})

		// Gateway proxy endpoints (API-key auth + rate limiting +
		// optional cloud middleware via WithGatewayMiddleware,
		// applied AFTER auth so wrapped middleware can read
		// APIKeyInfo / CustomerID from request context).
		r.Group(func(r chi.Router) {
			r.Use(authSvc.Middleware)
			r.Use(rateLimiter.Middleware)
			// Optional IP threat-list check. After auth/rate-limit per
			// the documented ordering (Auth → RateLimit → context
			// middleware → handler) and before the WithGatewayMiddleware
			// hooks so cloud middleware can read the verdict from the
			// request context. Health endpoints live outside this group
			// and are additionally guarded inside the middleware itself.
			if s.ipList != nil {
				r.Use(iplist.Middleware(s.ipList, iplist.MiddlewareConfig{Block: s.cfg.IPListBlock}))
			}
			for _, mw := range s.opts.gatewayMiddleware {
				r.Use(mw)
			}
			r.Post("/chat/completions", gw.ChatCompletions)
			r.Post("/guard/{proxyId}/v1/messages", gw.AnthropicMessages)

			// OTLP/HTTP trace ingestion. POST is authenticated; GET
			// /v1/traces (in the dashboard group above) lists traces.
			otlp := observability.NewOTLPHandler(s.recorder)
			r.Post("/traces", otlp.ServeHTTP)
		})

		// Caller extensions (e.g. cloud billing, cloud analytics).
		for _, fn := range s.opts.apiExtenders {
			fn(r)
		}
	})

	// SCIM 2.0 (governance user/group sync) lives in bastio-cloud and
	// is mounted there at /scim/v2 via Server.Router() after
	// server.New returns. OSS deployments don't expose it.

	// Public branded chat — slug-based form at /c/<slug>/... Visible
	// without dashboard auth so end users can chat. Custom-domain form
	// is wired separately as a Host-intercept middleware in
	// registerHostInterceptors so a customer's CNAME hits the right
	// branded workspace at their own root.
	r.Mount("/c", workspaceHandler.PublicRoutes())
	s.workspaceHandler = workspaceHandler
}

func (s *Server) registerDashboard(r chi.Router) {
	if s.opts.dashboardFS == nil {
		return
	}
	r.NotFound(SPAHandler(s.opts.dashboardFS).ServeHTTP)
	slog.Info("serving embedded dashboard")
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{
		"postgres":   "ok",
		"redis":      "ok",
		"clickhouse": "ok",
	}
	if err := s.db.Ping(r.Context()); err != nil {
		checks["postgres"] = err.Error()
	}
	if err := s.redis.Ping(r.Context()); err != nil {
		checks["redis"] = err.Error()
	}
	if err := s.ch.Ping(r.Context()); err != nil {
		checks["clickhouse"] = err.Error()
	}

	healthy := checks["postgres"] == "ok" && checks["redis"] == "ok" && checks["clickhouse"] == "ok"
	status := "healthy"
	code := http.StatusOK
	if !healthy {
		status = "unhealthy"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{
		"status":     status,
		"postgres":   checks["postgres"],
		"redis":      checks["redis"],
		"clickhouse": checks["clickhouse"],
	})
}

func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	// Postgres is the source of truth — every endpoint hits it.
	// Redis is required for rate limiting, session lookup, and cache;
	// without it, gateway requests fail intermittently rather than
	// cleanly. Both must be live for the load balancer to route
	// traffic here. ClickHouse is intentionally NOT checked: it's
	// analytics-only and the gateway degrades gracefully (writes
	// queue or drop, never block) when it's down. /health covers
	// the full surface for operators.
	if err := s.db.Ping(r.Context()); err != nil {
		http.Error(w, `{"status":"not ready","reason":"postgres"}`, http.StatusServiceUnavailable)
		return
	}
	if err := s.redis.Ping(r.Context()); err != nil {
		http.Error(w, `{"status":"not ready","reason":"redis"}`, http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// SetupLogger configures the default slog handler for JSON output at the
// requested level. Provided as a package helper so both OSS and cloud binaries
// can share the same logger setup.
func SetupLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))
}

// ossTenantKeyResolver is the default workspace.KeyResolver wired into
// OSS deployments. It reads from proxy_provider_keys via proxy.Service,
// preferring the explicit-default row and falling back to any per-proxy
// key the customer has stored — same source the dashboard's Provider
// Keys page writes to. Cloud overrides this through
// WorkspaceCustomizer.SetKeyResolver with a per-customer resolver that
// also honors billing-mode (BYO keys vs Bastio-platform keys).
type ossTenantKeyResolver struct {
	proxies *proxy.Service
}

func (r *ossTenantKeyResolver) ResolveKey(ctx context.Context, customerID uuid.UUID, provider string) (string, error) {
	return r.proxies.ResolveTenantKey(ctx, customerID, providers.Provider(provider))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
