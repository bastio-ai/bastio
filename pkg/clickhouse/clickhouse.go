package clickhouse

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// CH wraps a ClickHouse connection.
type CH struct {
	Conn driver.Conn
}

// New creates a new ClickHouse connection.
func New(ctx context.Context, clickhouseURL string) (*CH, error) {
	u, err := url.Parse(clickhouseURL)
	if err != nil {
		return nil, fmt.Errorf("parse clickhouse url: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "9000"
	}

	addr := fmt.Sprintf("%s:%s", host, port)

	// If the URL uses HTTP port 8123, use the native port 9000
	if port == "8123" {
		addr = fmt.Sprintf("%s:9000", host)
	}

	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: "bastio",
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
	}

	if u.User != nil {
		opts.Auth.Username = u.User.Username()
		if pwd, ok := u.User.Password(); ok {
			opts.Auth.Password = pwd
		}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	slog.Info("connected to clickhouse")
	return &CH{Conn: conn}, nil
}

// Ping checks the ClickHouse connection.
func (c *CH) Ping(ctx context.Context) error {
	return c.Conn.Ping(ctx)
}

// Close closes the ClickHouse connection.
func (c *CH) Close() error {
	return c.Conn.Close()
}

// Exec executes a query without returning rows.
func (c *CH) Exec(ctx context.Context, query string, args ...any) error {
	return c.Conn.Exec(ctx, query, args...)
}

// Migrate runs embedded ClickHouse migrations.
// CH doesn't support transactions, so each statement is executed individually.
// Uses a tracking table to avoid re-running migrations.
func (c *CH) Migrate(ctx context.Context) error {
	// Ensure bastio database exists
	if err := c.Conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS bastio"); err != nil {
		return fmt.Errorf("create database: %w", err)
	}

	// Create migration tracking table
	if err := c.Conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS bastio.schema_migrations (
			version String,
			applied_at DateTime DEFAULT now()
		) ENGINE = MergeTree() ORDER BY version
	`); err != nil {
		return fmt.Errorf("create ch migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read ch migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		version := entry.Name()

		// Check if already applied
		var count uint64
		if err := c.Conn.QueryRow(ctx,
			"SELECT count() FROM bastio.schema_migrations WHERE version = ?", version,
		).Scan(&count); err != nil {
			return fmt.Errorf("check ch migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		// Read migration file
		sql, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("read ch migration %s: %w", version, err)
		}

		slog.Info("applying clickhouse migration", "version", version)

		// Execute each statement separately (CH doesn't support multi-statement)
		for _, stmt := range splitStatements(string(sql)) {
			// Strip leading comment lines to get to the actual SQL
			cleaned := stripLeadingComments(stmt)
			if cleaned == "" {
				continue
			}
			if err := c.Conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("exec ch migration %s: %w\nstatement: %s", version, err, truncate(stmt, 200))
			}
		}

		// Record migration
		if err := c.Conn.Exec(ctx,
			"INSERT INTO bastio.schema_migrations (version) VALUES (?)", version,
		); err != nil {
			return fmt.Errorf("record ch migration %s: %w", version, err)
		}

		slog.Info("applied clickhouse migration", "version", version)
	}

	return nil
}

// splitStatements splits SQL on semicolons, respecting comments.
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			current.WriteString(line)
			current.WriteString("\n")
			continue
		}
		if strings.HasSuffix(trimmed, ";") {
			current.WriteString(line)
			current.WriteString("\n")
			stmts = append(stmts, current.String())
			current.Reset()
		} else {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// stripLeadingComments removes leading comment and blank lines,
// returning the remaining SQL. Returns empty string if only comments.
func stripLeadingComments(s string) string {
	var result strings.Builder
	foundSQL := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if !foundSQL {
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			foundSQL = true
		}
		result.WriteString(line)
		result.WriteString("\n")
	}
	return strings.TrimSpace(result.String())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
