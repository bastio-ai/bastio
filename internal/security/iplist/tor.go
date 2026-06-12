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

// DefaultTorExitURL is the Tor Project's bulk exit-node list: one
// plain IP per line, no comments in practice (we tolerate them
// anyway). Same primary source v1 used; v1's fallback mirror
// (dan.me.uk) is rate-limited and intentionally not ported.
const DefaultTorExitURL = "https://check.torproject.org/torbulkexitlist"

// Tor fetches and parses the Tor exit-node list.
type Tor struct {
	name   string
	url    string
	client *http.Client
}

// NewTor builds the provider. url == "" uses the public bulk exit
// list; tests point it at an httptest server. client == nil uses a
// dedicated client with a generous timeout.
func NewTor(url string, client *http.Client) *Tor {
	if url == "" {
		url = DefaultTorExitURL
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Tor{name: "tor_exits", url: url, client: client}
}

// Name implements Provider.
func (t *Tor) Name() string { return t.name }

// Fetch implements Provider.
func (t *Tor) Fetch(ctx context.Context) ([]netip.Prefix, error) {
	body, err := fetchFeed(ctx, t.client, t.url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseExitList(body)
}

// parseExitList parses the plain-IP-per-line exit list into host
// prefixes (/32 or /128). Invalid lines are skipped.
func parseExitList(r io.Reader) ([]netip.Prefix, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []netip.Prefix
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		a, err := netip.ParseAddr(line)
		if err != nil {
			continue
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan exit list: %w", err)
	}
	return out, nil
}
