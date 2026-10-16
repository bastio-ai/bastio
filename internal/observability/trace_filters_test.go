package observability

import (
	"net/url"
	"strings"
	"testing"
)

func TestTraceExplorerFiltersRunBeforeLimitAndStayTenantScoped(t *testing.T) {
	q := url.Values{"trace_name_contains": {"card_refresh' OR 1=1"}, "provider": {"openrouter"}, "trace_type": {"embedding"}, "security": {"threat"}, "status": {"error,blocked,rate_limited"}, "tag": {"feature:checkout"}, "limit": {"100"}}
	sql, args := buildTracesQuery(testTenantID, q)
	if !strings.Contains(sql, "WHERE customer_id = toUUID(?)") || args[0] != testTenantID {
		t.Fatal("missing tenant scope")
	}
	if strings.Contains(sql, q.Get("trace_name_contains")) {
		t.Fatal("caller value interpolated into SQL")
	}
	for _, predicate := range []string{"positionCaseInsensitive(trace_name, ?) > 0", "threat_detected = true", "status IN (?,?,?)", traceTypeSQL + " = ?", "tags[?] = ?"} {
		index := strings.Index(sql, predicate)
		if index < 0 || index >= strings.Index(sql, "ORDER BY") {
			t.Fatalf("filter missing before pagination: %s", predicate)
		}
	}
}

func TestTraceExplorerKeepsExactNameFilterAndClampsNegativeOffset(t *testing.T) {
	sql, args := buildTracesQuery(testTenantID, url.Values{"trace_name": {"plendio.card_refresh"}, "security": {"clean"}, "offset": {"-1"}})
	if !strings.Contains(sql, "trace_name = ?") || !strings.Contains(sql, "threat_detected = false") {
		t.Fatal("exact filters changed")
	}
	if args[len(args)-1] != 0 {
		t.Fatal("negative offset not clamped")
	}
}
