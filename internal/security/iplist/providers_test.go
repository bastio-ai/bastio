package iplist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

// fixtureServer serves a testdata file at every path.
func fixtureServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func prefixSet(prefixes []netip.Prefix) map[string]bool {
	out := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		out[p.String()] = true
	}
	return out
}

func TestFireHOLFetchParsesNetset(t *testing.T) {
	srv := fixtureServer(t, "firehol_level1.netset")
	f := NewFireHOL(srv.URL, srv.Client())

	if f.Name() != "firehol_level1" {
		t.Fatalf("name = %q, want firehol_level1", f.Name())
	}

	prefixes, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	got := prefixSet(prefixes)
	want := []string{
		"192.0.2.0/24",         // plain CIDR
		"198.51.100.7/32",      // bare IPv4 → /32
		"203.0.113.0/25",       // non-octet boundary CIDR
		"10.0.0.0/8",           // overlapping pair kept at parse time
		"10.1.0.0/16",          // (merged later by compile)
		"2001:db8::/32",        // IPv6 CIDR
		"2001:db8:ffff::1/128", // bare IPv6 → /128
		"1.2.3.0/24",           // trailing inline comment stripped
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing prefix %s in %v", w, prefixes)
		}
	}
	// Comments, blanks, junk hostnames, out-of-range octets, and /33
	// must all be skipped: exactly the wanted entries survive.
	if len(prefixes) != len(want) {
		t.Errorf("parsed %d prefixes, want %d: %v", len(prefixes), len(want), prefixes)
	}
}

func TestTorFetchParsesExitList(t *testing.T) {
	srv := fixtureServer(t, "tor_exits.txt")
	tor := NewTor(srv.URL, srv.Client())

	if tor.Name() != "tor_exits" {
		t.Fatalf("name = %q, want tor_exits", tor.Name())
	}

	prefixes, err := tor.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	got := prefixSet(prefixes)
	want := []string{
		"185.220.101.1/32",
		"185.220.101.2/32",
		"199.249.230.87/32",
		"2620:7:6001::1/128",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing prefix %s in %v", w, prefixes)
		}
	}
	if len(prefixes) != len(want) {
		t.Errorf("parsed %d prefixes, want %d: %v", len(prefixes), len(want), prefixes)
	}
}

func TestFetchNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	if _, err := NewFireHOL(srv.URL, srv.Client()).Fetch(context.Background()); err == nil {
		t.Fatal("firehol fetch: want error on 429, got nil")
	}
	if _, err := NewTor(srv.URL, srv.Client()).Fetch(context.Background()); err == nil {
		t.Fatal("tor fetch: want error on 429, got nil")
	}
}

func TestDefaultURLs(t *testing.T) {
	if f := NewFireHOL("", nil); f.url != DefaultFireHOLLevel1URL {
		t.Errorf("firehol default url = %q", f.url)
	}
	if tor := NewTor("", nil); tor.url != DefaultTorExitURL {
		t.Errorf("tor default url = %q", tor.url)
	}
}
