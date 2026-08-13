package observability

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

const testTenantID = "00000000-0000-0000-0000-000000000001"

// TestBuildThreatsQuery_TenantAlwaysFirst guards the multi-tenancy
// invariant: every query path must start with customer_id = ? and that
// value must be the first positional arg. A regression here would silently
// leak data across tenants.
func TestBuildThreatsQuery_TenantAlwaysFirst(t *testing.T) {
	cases := []url.Values{
		{},
		{"severity": []string{"high"}},
		{"threat_type": []string{"jailbreak"}, "action_taken": []string{"block"}},
		{"search": []string{"leak me"}},
		{"from": []string{"2026-01-01T00:00:00Z"}, "to": []string{"2026-02-01T00:00:00Z"}},
		{"sort": []string{"score"}, "order": []string{"asc"}},
		{"limit": []string{"500"}, "offset": []string{"10"}},
	}
	for _, q := range cases {
		sql, args := buildThreatsQuery(testTenantID, q)
		if !strings.Contains(sql, "WHERE customer_id = toUUID(?)") {
			t.Fatalf("missing tenant WHERE for %v\nSQL: %s", q, sql)
		}
		if len(args) == 0 || args[0] != testTenantID {
			t.Fatalf("first arg should be tenant id, got %v for %v", args, q)
		}
	}
}

func TestBuildThreatsQuery_LimitClamping(t *testing.T) {
	cases := []struct {
		name, limit string
		wantLimit   int
	}{
		{"default when missing", "", 50},
		{"default when zero", "0", 50},
		{"default when negative", "-5", 50},
		{"clamps above 200", "9999", 50},
		{"accepts valid", "42", 42},
		{"accepts exactly 200", "200", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{}
			if tc.limit != "" {
				q.Set("limit", tc.limit)
			}
			_, args := buildThreatsQuery(testTenantID, q)
			gotLimit := args[len(args)-2] // ...LIMIT ? OFFSET ?
			if gotLimit != tc.wantLimit {
				t.Fatalf("limit: got %v, want %d", gotLimit, tc.wantLimit)
			}
		})
	}
}

func TestBuildThreatsQuery_OffsetClamping(t *testing.T) {
	q := url.Values{"offset": []string{"-10"}}
	_, args := buildThreatsQuery(testTenantID, q)
	if got := args[len(args)-1]; got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %v", got)
	}
}

func TestBuildThreatsQuery_ExactMatchFilters(t *testing.T) {
	cases := []struct {
		param, value, column string
	}{
		{"severity", "high", "severity"},
		{"threat_type", "jailbreak", "threat_type"},
		{"detector_name", "regex_pii_v1", "detector_name"},
		{"action_taken", "block", "action_taken"},
		{"end_user_id", "user-42", "end_user_id"},
		{"ip_address", "10.0.0.1", "ip_address"},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			q := url.Values{tc.param: []string{tc.value}}
			sql, args := buildThreatsQuery(testTenantID, q)
			want := " AND " + tc.column + " = ?"
			if !strings.Contains(sql, want) {
				t.Fatalf("missing clause %q in %s", want, sql)
			}
			// customer_id, value, limit, offset.
			if len(args) != 4 {
				t.Fatalf("expected 4 args, got %d: %v", len(args), args)
			}
			if args[1] != tc.value {
				t.Fatalf("filter value: got %v, want %q", args[1], tc.value)
			}
		})
	}
}

func TestBuildThreatsQuery_MultiValueSeverity(t *testing.T) {
	q := url.Values{"severity": []string{"critical,high"}}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "severity IN (?,?)") {
		t.Fatalf("expected IN clause: %s", sql)
	}
	// customer_id, critical, high, limit, offset.
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}
	if args[1] != "critical" || args[2] != "high" {
		t.Fatalf("severity values mis-ordered: %v", args)
	}
}

func TestBuildThreatsQuery_MultiValueThreatType(t *testing.T) {
	q := url.Values{"threat_type": []string{"jailbreak,injection,pii"}}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "threat_type IN (?,?,?)") {
		t.Fatalf("expected IN clause: %s", sql)
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}
}

func TestBuildThreatsQuery_CommaSeparatedIgnoresEmpties(t *testing.T) {
	q := url.Values{"severity": []string{"critical, ,high,"}}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "severity IN (?,?)") {
		t.Fatalf("expected 2-value IN clause: %s", sql)
	}
	if len(args) != 5 || args[1] != "critical" || args[2] != "high" {
		t.Fatalf("wrong values from comma splitting: %v", args)
	}
}

func TestBuildThreatsQuery_TimeRange(t *testing.T) {
	q := url.Values{
		"from": []string{"2026-04-01T00:00:00Z"},
		"to":   []string{"2026-04-21T00:00:00Z"},
	}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "detected_at >= parseDateTime64BestEffort(?)") {
		t.Fatalf("missing from clause: %s", sql)
	}
	if !strings.Contains(sql, "detected_at <= parseDateTime64BestEffort(?)") {
		t.Fatalf("missing to clause: %s", sql)
	}
	// customer_id, from, to, limit, offset.
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}
}

func TestBuildThreatsQuery_EnvironmentRemainsTenantScoped(t *testing.T) {
	q := url.Values{"environment": []string{"production"}}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "trace_id IN (SELECT id FROM bastio.analytics_request_logs WHERE customer_id = toUUID(?) AND environment = ?)") {
		t.Fatalf("missing tenant-scoped environment clause: %s", sql)
	}
	// Outer tenant, subquery tenant, environment, limit, offset.
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}
	if args[1] != testTenantID || args[2] != "production" {
		t.Fatalf("environment subquery must bind tenant before environment, got %v", args)
	}
}

func TestBuildThreatsQuery_SearchBindsTwice(t *testing.T) {
	q := url.Values{"search": []string{"ignore previous"}}
	sql, args := buildThreatsQuery(testTenantID, q)
	if !strings.Contains(sql, "positionCaseInsensitive(matched_pattern, ?)") {
		t.Fatalf("missing matched_pattern search: %s", sql)
	}
	if !strings.Contains(sql, "positionCaseInsensitive(matched_content, ?)") {
		t.Fatalf("missing matched_content search: %s", sql)
	}
	// customer_id, search, search, limit, offset — bound twice so the
	// same value is compared against both columns.
	if len(args) != 5 {
		t.Fatalf("expected 5 args (customer + 2x search + limit + offset), got %d: %v", len(args), args)
	}
	if args[1] != "ignore previous" || args[2] != "ignore previous" {
		t.Fatalf("search should be bound twice identically, got %v and %v", args[1], args[2])
	}
}

func TestBuildThreatsQuery_SortAllowList(t *testing.T) {
	cases := []struct {
		sort, want string
	}{
		{"detected_at", "ORDER BY detected_at DESC"},
		{"score", "ORDER BY score DESC"},
		{"confidence", "ORDER BY confidence DESC"},
		{"severity", "ORDER BY multiIf(severity = 'critical', 0"},
		// Unknown sort values fall back to detected_at — never
		// interpolated as-is, which would be a SQL-injection foothold.
		{"", "ORDER BY detected_at DESC"},
		{"bogus_column", "ORDER BY detected_at DESC"},
		{"; DROP TABLE", "ORDER BY detected_at DESC"},
	}
	for _, tc := range cases {
		t.Run(tc.sort, func(t *testing.T) {
			q := url.Values{"sort": []string{tc.sort}}
			sql, _ := buildThreatsQuery(testTenantID, q)
			if !strings.Contains(sql, tc.want) {
				t.Fatalf("sort=%q: expected %q in SQL, got: %s", tc.sort, tc.want, sql)
			}
		})
	}
}

func TestBuildThreatsQuery_OrderDirection(t *testing.T) {
	asc, _ := buildThreatsQuery(testTenantID, url.Values{"sort": []string{"score"}, "order": []string{"asc"}})
	if !strings.Contains(asc, "ORDER BY score ASC") {
		t.Fatalf("expected ASC: %s", asc)
	}
	desc, _ := buildThreatsQuery(testTenantID, url.Values{"sort": []string{"score"}, "order": []string{"desc"}})
	if !strings.Contains(desc, "ORDER BY score DESC") {
		t.Fatalf("expected DESC: %s", desc)
	}
	// Case-insensitive.
	upper, _ := buildThreatsQuery(testTenantID, url.Values{"sort": []string{"score"}, "order": []string{"ASC"}})
	if !strings.Contains(upper, "ORDER BY score ASC") {
		t.Fatalf("ASC (upper) not honored: %s", upper)
	}
	// Garbage falls back to DESC, never interpolated.
	garbage, _ := buildThreatsQuery(testTenantID, url.Values{"sort": []string{"score"}, "order": []string{"sideways; DROP"}})
	if !strings.Contains(garbage, "ORDER BY score DESC") {
		t.Fatalf("bogus order should fall back to DESC: %s", garbage)
	}
}

// TestGetThreat_RejectsInvalidUUID guards the input validator on the
// detail endpoint. We don't touch ClickHouse here — the guard short-
// circuits before the query — so no client is needed.
func TestGetThreat_RejectsInvalidUUID(t *testing.T) {
	h := &Handler{} // ch stays nil; the invalid-UUID path returns before any query.
	r := chi.NewRouter()
	r.Get("/v1/threats/{id}", h.GetThreat)

	for _, id := range []string{"not-a-uuid", "123", "' OR 1=1"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/threats/"+url.PathEscape(id), nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("id=%q: expected 400, got %d: %s", id, rr.Code, rr.Body.String())
		}
	}
}

func TestBuildThreatsQuery_EmptyParamsAreIgnored(t *testing.T) {
	// Empty values should not append empty WHERE clauses or stray args.
	q := url.Values{
		"severity":    []string{""},
		"threat_type": []string{""},
		"search":      []string{""},
		"from":        []string{""},
		"to":          []string{""},
	}
	sql, args := buildThreatsQuery(testTenantID, q)
	if strings.Contains(sql, "severity = ?") {
		t.Fatalf("empty severity should not produce a filter: %s", sql)
	}
	if len(args) != 3 {
		// customer_id, limit, offset.
		t.Fatalf("expected 3 args when all filters empty, got %d: %v", len(args), args)
	}
}
