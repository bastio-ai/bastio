package iplist

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// DefaultFireHOLLevel1URL is the public FireHOL level1 netset — the
// "highly recommended" baseline blocklist (bogons, hijacked ranges,
// top attackers). Same source v1 used.
const DefaultFireHOLLevel1URL = "https://raw.githubusercontent.com/firehol/blocklist-ipsets/master/firehol_level1.netset"

// FireHOL fetches and parses a FireHOL .netset feed: '#' comment lines
// plus one CIDR or bare IP per line.
type FireHOL struct {
	name   string
	url    string
	client *http.Client
}

// NewFireHOL builds the provider. url == "" uses the public level1
// netset; tests point it at an httptest server. client == nil uses a
// dedicated client with a generous timeout (feeds are a few MB).
func NewFireHOL(url string, client *http.Client) *FireHOL {
	if url == "" {
		url = DefaultFireHOLLevel1URL
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &FireHOL{name: "firehol_level1", url: url, client: client}
}

// Name implements Provider.
func (f *FireHOL) Name() string { return f.name }

// Fetch implements Provider.
func (f *FireHOL) Fetch(ctx context.Context) ([]netip.Prefix, error) {
	body, err := fetchFeed(ctx, f.client, f.url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseNetset(body)
}

// parseNetset parses the FireHOL netset format. Lines are either
// comments (starting with '#' or ';'), blank, a CIDR (1.2.3.0/24), or
// a bare IP (1.2.3.4 → /32, IPv6 → /128). Unparseable lines are
// skipped — feeds occasionally carry junk and a single bad line must
// not poison the refresh.
func parseNetset(r io.Reader) ([]netip.Prefix, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []netip.Prefix
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// Strip trailing inline comments ("1.2.3.0/24 # spamhaus").
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if p, ok := parsePrefixOrAddr(line); ok {
			out = append(out, p)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan netset: %w", err)
	}
	return out, nil
}

// parsePrefixOrAddr parses a CIDR or bare IP into a masked prefix.
// Bare addresses become host prefixes (/32 or /128); IPv4-mapped IPv6
// is unmapped so all v4 entries land in the same comparison family.
func parsePrefixOrAddr(s string) (netip.Prefix, bool) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, false
		}
		return p.Masked(), true
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, false
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), true
}

// fetchFeed GETs a feed URL and returns the body on a 200. Shared by
// all providers in this package.
func fetchFeed(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build feed request: %w", err)
	}
	req.Header.Set("User-Agent", "bastio-iplist/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch feed %s: unexpected status %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}
