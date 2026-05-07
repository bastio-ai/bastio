package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Context keys for branded-chat requests.
type brandedCtxKey int

const (
	brandedSlugKey      brandedCtxKey = iota + 1
	brandedSessionIDKey               // uuid.UUID — owning session row
	brandedSessionTokenKey            // string — raw cookie value (set on first request)
)

// brandedSessionCookie is the cookie name used for the anonymous chat
// session. Long-lived (30d) but Secure+HttpOnly so it can't be read by
// page JS or shipped over plain HTTP.
const brandedSessionCookie = "bastio_chat_session"

// PublicRoutes returns the unauthenticated branded-chat router. Mounted
// outside the dashboard auth group so end users hit it without a login.
// Two address forms:
//
//	workspace.bastio.com/c/<slug>      (path-based)
//	<custom-domain>/                   (Host-based, customer's CNAME)
func (h *Handler) PublicRoutes() http.Handler {
	r := chi.NewRouter()

	// Path-form (hosted at workspace.bastio.com/c/<slug>).
	r.Route("/{slug}", func(r chi.Router) {
		r.Use(h.brandedSlugMiddleware)
		r.Use(h.brandedSessionMiddleware)
		r.Get("/", h.brandedPage)
		r.Post("/session/label", h.brandedSetLabel)
		r.Post("/messages", h.brandedSend)
		r.Post("/messages/stream", h.brandedStream)
	})

	return r
}

// HostRoutes is mounted at "/" on the bastio server and handles Host-based
// custom-domain branded chat. Custom-domain users hit `/` on their own
// domain; the resolver finds the customer by Host header.
func (h *Handler) HostRoutes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.brandedHostMiddleware)
	r.Use(h.brandedSessionMiddleware)
	r.Get("/", h.brandedPage)
	r.Post("/session/label", h.brandedSetLabel)
	r.Post("/messages", h.brandedSend)
	r.Post("/messages/stream", h.brandedStream)
	return r
}

// =============================================================================
// Resolver middlewares
// =============================================================================

func (h *Handler) brandedSlugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		cid, err := h.store.CustomerBySlug(r.Context(), slug)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), CustomerIDKey, cid)
		ctx = context.WithValue(ctx, brandedSlugKey, slug)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) brandedHostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid, err := h.store.CustomerByDomain(r.Context(), r.Host)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), CustomerIDKey, cid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// brandedSessionMiddleware reads-or-creates the chat session cookie and
// stuffs the session id + raw token into the request context. The
// downstream handlers can read the session id (for conversation
// scoping) and the raw token is set as a cookie before any response
// body is written.
func (h *Handler) brandedSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid, _ := r.Context().Value(CustomerIDKey).(uuid.UUID)
		if cid == uuid.Nil {
			http.NotFound(w, r)
			return
		}
		var raw string
		if c, err := r.Cookie(brandedSessionCookie); err == nil {
			raw = c.Value
		}
		sess, finalToken, _, err := h.store.EnsureBrandedSession(
			r.Context(), cid, raw, r.UserAgent(), pseudonymizeIP(r.RemoteAddr),
		)
		if err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		// Refresh cookie (or set it for the first time).
		http.SetCookie(w, &http.Cookie{
			Name:     brandedSessionCookie,
			Value:    finalToken,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   30 * 24 * 3600,
		})
		ctx := context.WithValue(r.Context(), brandedSessionIDKey, sess.ID)
		ctx = context.WithValue(ctx, brandedSessionTokenKey, finalToken)
		// Stuff a stable user_id into the dashboard's UserIDKey too so
		// the rest of the handler stack threads correctly.
		ctx = context.WithValue(ctx, UserIDKey, "branded:"+sess.ID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func brandedSessionFromCtx(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(brandedSessionIDKey).(uuid.UUID)
	return v, ok && v != uuid.Nil
}

// pseudonymizeIP hashes the client IP so the session row can correlate
// repeated visits from the same network without storing PII.
func pseudonymizeIP(remoteAddr string) string {
	if i := strings.LastIndex(remoteAddr, ":"); i > 0 {
		remoteAddr = remoteAddr[:i]
	}
	if remoteAddr == "" {
		return ""
	}
	h := sha256.Sum256([]byte("bastio:branded:" + remoteAddr))
	return hex.EncodeToString(h[:])[:16]
}

// =============================================================================
// Branded page (server-side HTML)
// =============================================================================

// brandedPage renders the chat HTML with branding pulled from
// workspace_settings.branding. Inline CSS + a tiny JS snippet — no
// build step, no React, fast first paint.
func (h *Handler) brandedPage(w http.ResponseWriter, r *http.Request) {
	cid, _ := r.Context().Value(CustomerIDKey).(uuid.UUID)
	settings, err := h.store.EnsureSettings(r.Context(), cid)
	if err != nil {
		http.Error(w, "settings unavailable", http.StatusInternalServerError)
		return
	}
	branding := decodeBranding(settings.Branding)

	slug, _ := r.Context().Value(brandedSlugKey).(string)
	apiBase := "/c/" + slug
	if slug == "" {
		// Custom-domain mode — endpoints live at the root of the host.
		apiBase = ""
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprint(w, renderBrandedHTML(branding, apiBase))
}

// brandedBranding is the typed view of the org's branding JSONB.
type brandedBranding struct {
	WorkspaceName  string `json:"workspace_name"`
	LogoURL        string `json:"logo_url"`
	PrimaryColor   string `json:"primary_color"`
	WelcomeMessage string `json:"welcome_message"`
	Placeholder    string `json:"placeholder"`
	FooterText     string `json:"footer_text"`
}

func decodeBranding(raw json.RawMessage) brandedBranding {
	b := brandedBranding{
		WorkspaceName:  "AI Workspace",
		PrimaryColor:   "#06b6d4",
		WelcomeMessage: "How can I help you today?",
		Placeholder:    "Send a message…",
	}
	if len(raw) == 0 {
		return b
	}
	_ = json.Unmarshal(raw, &b)
	return b
}

// =============================================================================
// Branded send (non-streaming + streaming)
// =============================================================================

type brandedSendRequest struct {
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	Content        string     `json:"content"`
}

// brandedSend creates or reuses a conversation owned by the visitor's
// session and writes one user message + one assistant reply.
func (h *Handler) brandedSend(w http.ResponseWriter, r *http.Request) {
	convID, body, ok := h.brandedPrepareConversation(w, r)
	if !ok {
		return
	}
	// Inject conversation id back into the URL parameter the existing
	// handler reads. We re-route through the dashboard send path so
	// the same persistence + provider call logic applies to branded.
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("id", convID.String())

	// Re-encode body the existing handler expects.
	r = withReplacedBody(r, SendMessageRequest{Content: body})
	h.sendMessage(w, r)
}

// brandedStream is the SSE variant — same flow, calls streamSendMessage.
func (h *Handler) brandedStream(w http.ResponseWriter, r *http.Request) {
	convID, body, ok := h.brandedPrepareConversation(w, r)
	if !ok {
		return
	}
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("id", convID.String())

	r = withReplacedBody(r, SendMessageRequest{Content: body})
	h.streamSendMessage(w, r)
}

// brandedPrepareConversation reads the request body and ensures a
// conversation row exists for this visitor session. Returns the
// conversation id + the user-supplied content, or false on error
// (response already written).
func (h *Handler) brandedPrepareConversation(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	cid, _ := r.Context().Value(CustomerIDKey).(uuid.UUID)
	sessID, ok := brandedSessionFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no session")
		return uuid.Nil, "", false
	}

	var body brandedSendRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return uuid.Nil, "", false
	}
	if strings.TrimSpace(body.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return uuid.Nil, "", false
	}

	// Bind the conversation to the visitor's user_id (synthesized from
	// the session id) so listing later returns only their chats.
	userID := "branded:" + sessID.String()

	if body.ConversationID != nil {
		// Validate ownership: must belong to this customer + user.
		conv, err := h.store.GetConversation(r.Context(), cid, *body.ConversationID)
		if err != nil {
			writeError(w, http.StatusNotFound, "conversation not found")
			return uuid.Nil, "", false
		}
		if conv.UserID != userID {
			writeError(w, http.StatusForbidden, "not your conversation")
			return uuid.Nil, "", false
		}
		return *body.ConversationID, body.Content, true
	}

	// First message in a brand-new conversation.
	settings, _ := h.store.EnsureSettings(r.Context(), cid)
	var assistantID *uuid.UUID
	if settings != nil && settings.DefaultAssistantID != nil {
		assistantID = settings.DefaultAssistantID
	}
	c, err := h.store.CreateConversation(r.Context(), Conversation{
		CustomerID:  cid,
		UserID:      userID,
		AssistantID: assistantID,
		Title:       firstWords(body.Content, 60),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return uuid.Nil, "", false
	}
	return c.ID, body.Content, true
}

// brandedSetLabel lets the visitor set a display name (optional). UI
// element shown next to the avatar circle.
func (h *Handler) brandedSetLabel(w http.ResponseWriter, r *http.Request) {
	sessID, ok := brandedSessionFromCtx(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no session")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.store.SetBrandedSessionLabel(r.Context(), sessID, body.Label); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// HTML page
// =============================================================================

func renderBrandedHTML(b brandedBranding, apiBase string) string {
	// Single-file page: minimal CSS variables driven by branding,
	// vanilla JS that handles SSE consumption without React. Keeps the
	// footprint <8 KB and the customer's chat URL feels instant.
	primaryEsc := html.EscapeString(b.PrimaryColor)
	if !strings.HasPrefix(primaryEsc, "#") && !strings.HasPrefix(primaryEsc, "rgb") {
		primaryEsc = "#06b6d4"
	}

	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(b.WorkspaceName) + `</title>
<style>
  :root {
    --primary: ` + primaryEsc + `;
    --bg: #0b0d0f;
    --bg-alt: #14171a;
    --fg: #e6e8ea;
    --fg-mute: #8b9097;
    --border: #25292e;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; background: var(--bg); color: var(--fg);
    font: 15px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  body { display: flex; flex-direction: column; }
  header { display: flex; align-items: center; gap: 12px; padding: 16px 24px;
    border-bottom: 1px solid var(--border); }
  header img { height: 28px; }
  header h1 { font-size: 16px; margin: 0; font-weight: 600; }
  main { flex: 1; max-width: 760px; width: 100%; margin: 0 auto;
    display: flex; flex-direction: column; }
  #log { flex: 1; padding: 24px; overflow-y: auto; }
  .msg { margin-bottom: 16px; max-width: 80%; padding: 10px 14px;
    border-radius: 10px; white-space: pre-wrap; font-family: inherit; }
  .msg.user { background: color-mix(in srgb, var(--primary) 15%, transparent);
    align-self: flex-end; margin-left: auto; }
  .msg.assistant { background: var(--bg-alt); }
  .msg.error { color: #fca5a5; background: color-mix(in srgb, #fca5a5 10%, transparent); }
  .welcome { color: var(--fg-mute); text-align: center; margin: 60px 24px; }
  form { display: flex; gap: 8px; padding: 16px 24px; border-top: 1px solid var(--border); }
  textarea { flex: 1; min-height: 44px; max-height: 200px; padding: 10px;
    border-radius: 8px; border: 1px solid var(--border); background: var(--bg-alt);
    color: var(--fg); font: inherit; resize: none; }
  textarea:focus { outline: none; border-color: var(--primary); }
  button { background: var(--primary); color: #001016; border: 0; border-radius: 8px;
    padding: 0 16px; font-weight: 600; cursor: pointer; }
  button:disabled { opacity: 0.5; cursor: default; }
  footer { color: var(--fg-mute); font-size: 12px; padding: 8px 24px; text-align: center; }
  footer a { color: var(--fg-mute); }
  .cursor { display: inline-block; width: 6px; height: 14px; vertical-align: text-bottom;
    background: currentColor; animation: blink 1s steps(2) infinite; }
  @keyframes blink { 50% { opacity: 0; } }
</style>
</head>
<body>
<header>
  ` + logoTag(b) + `
  <h1>` + html.EscapeString(b.WorkspaceName) + `</h1>
</header>
<main>
  <div id="log">
    <div class="welcome">` + html.EscapeString(b.WelcomeMessage) + `</div>
  </div>
  <form id="composer">
    <textarea id="draft" rows="1" placeholder="` + html.EscapeString(b.Placeholder) + `"></textarea>
    <button type="submit" id="send">Send</button>
  </form>
  <footer>` + footerTag(b) + `</footer>
</main>
<script>
(function () {
  var apiBase = ` + jsString(apiBase) + `;
  var log = document.getElementById("log");
  var welcome = log.querySelector(".welcome");
  var draft = document.getElementById("draft");
  var send = document.getElementById("send");
  var conversationID = null;

  function bubble(role, text) {
    var el = document.createElement("pre");
    el.className = "msg " + role;
    el.textContent = text;
    log.appendChild(el);
    log.scrollTop = log.scrollHeight;
    return el;
  }

  // mdToHTML — minimal client-side markdown renderer for assistant
  // responses. Hand-rolled to avoid pulling a CDN dependency on a
  // surface that customers brand as their own (privacy + perf).
  // Covers fenced code blocks, inline code, bold, italic, headings,
  // bullet/numbered lists, and paragraphs. Anything more exotic
  // falls through as plain text. Input is HTML-escaped first; the
  // markdown patterns then introduce trusted tags.
  function mdToHTML(src) {
    var esc = src
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");

    // Fenced code blocks (triple-tick lang ... triple-tick). Backticks
    // are written via \x60 because this whole script body lives inside
    // a Go raw string literal that's also delimited by backticks.
    esc = esc.replace(/\x60\x60\x60(\w*)\n([\s\S]*?)\n\x60\x60\x60/g, function (_m, lang, body) {
      return '<pre class="code"><code>' + body + '</code></pre>';
    });

    // Inline single-tick code spans.
    esc = esc.replace(/\x60([^\x60\n]+)\x60/g, '<code>$1</code>');

    // Bold **…** and italic *…*.
    esc = esc.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
    esc = esc.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');

    // Headings — line starts with #/##/###.
    esc = esc.replace(/^### (.+)$/gm, '<h3>$1</h3>');
    esc = esc.replace(/^## (.+)$/gm, '<h2>$1</h2>');
    esc = esc.replace(/^# (.+)$/gm, '<h1>$1</h1>');

    // Bullet lists — runs of lines starting with "- " or "* ".
    esc = esc.replace(/(^|\n)((?:[-*] .+\n?)+)/g, function (_m, p, lines) {
      var items = lines.trim().split(/\n/).map(function (l) {
        return '<li>' + l.replace(/^[-*] /, '') + '</li>';
      }).join('');
      return p + '<ul>' + items + '</ul>';
    });

    // Numbered lists — runs of "1. text" lines.
    esc = esc.replace(/(^|\n)((?:\d+\. .+\n?)+)/g, function (_m, p, lines) {
      var items = lines.trim().split(/\n/).map(function (l) {
        return '<li>' + l.replace(/^\d+\. /, '') + '</li>';
      }).join('');
      return p + '<ol>' + items + '</ol>';
    });

    // Paragraph wrap on remaining double-newline-delimited blocks
    // that aren't already block-level.
    return esc
      .split(/\n{2,}/)
      .map(function (block) {
        if (/^<(h\d|ul|ol|pre|blockquote)/.test(block.trim())) return block;
        return '<p>' + block.replace(/\n/g, '<br>') + '</p>';
      })
      .join('\n');
  }

  async function streamSend(content) {
    if (welcome) { welcome.remove(); welcome = null; }
    bubble("user", content);
    var assistant = bubble("assistant", "");
    var cursor = document.createElement("span");
    cursor.className = "cursor";
    assistant.appendChild(cursor);

    var resp;
    try {
      resp = await fetch(apiBase + "/messages/stream", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Accept": "text/event-stream" },
        body: JSON.stringify({ conversation_id: conversationID, content: content }),
      });
    } catch (e) {
      assistant.classList.add("error"); assistant.textContent = "Network error: " + e.message;
      return;
    }
    if (!resp.ok || !resp.body) {
      assistant.classList.add("error");
      assistant.textContent = "Server error " + resp.status;
      return;
    }

    var reader = resp.body.getReader();
    var dec = new TextDecoder();
    var buf = "";
    var text = "";
    while (true) {
      var chunk = await reader.read();
      if (chunk.done) break;
      buf += dec.decode(chunk.value, { stream: true });
      var idx;
      while ((idx = buf.indexOf("\n\n")) !== -1) {
        var frame = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        var ev = "message", data = "";
        frame.split("\n").forEach(function (line) {
          if (line.indexOf("event:") === 0) ev = line.slice(6).trim();
          else if (line.indexOf("data:") === 0) data += line.slice(5).trim();
        });
        if (!data) continue;
        try { data = JSON.parse(data); } catch (e) { continue; }
        if (ev === "token" && typeof data.delta === "string") {
          text += data.delta;
          assistant.textContent = text;
          assistant.appendChild(cursor);
        } else if (ev === "done" && data.message) {
          conversationID = data.message.conversation_id;
          if (cursor.parentNode) cursor.remove();
          if (data.message.error) {
            assistant.classList.add("error");
          } else {
            // Final message — swap the plain-text streamed body for
            // a rendered markdown version. Keeps mid-stream display
            // glitch-free (partial fences, half-finished lists)
            // while delivering markdown polish on the result.
            assistant.innerHTML = mdToHTML(text);
          }
        } else if (ev === "error" && data.error) {
          assistant.classList.add("error"); assistant.textContent = data.error;
          if (cursor.parentNode) cursor.remove();
        }
      }
    }
    if (cursor.parentNode) cursor.remove();
  }

  document.getElementById("composer").addEventListener("submit", function (e) {
    e.preventDefault();
    var content = draft.value.trim();
    if (!content) return;
    draft.value = "";
    send.disabled = true;
    streamSend(content).finally(function () { send.disabled = false; draft.focus(); });
  });

  draft.addEventListener("keydown", function (e) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      document.getElementById("composer").requestSubmit();
    }
  });
})();
</script>
</body>
</html>`
}

func logoTag(b brandedBranding) string {
	if b.LogoURL == "" {
		return ""
	}
	return `<img src="` + html.EscapeString(b.LogoURL) + `" alt="">`
}

func footerTag(b brandedBranding) string {
	if b.FooterText != "" {
		return html.EscapeString(b.FooterText)
	}
	return `Powered by <a href="https://bastio.com" target="_blank" rel="noopener">Bastio</a>`
}

// jsString escapes a Go string so it's safe to inject into a JS string
// literal in the rendered HTML.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// withReplacedBody returns r with its body replaced by the JSON-encoded
// value v. Used by the branded send handlers to forward through the
// existing dashboard send-message logic with a normalized body shape.
func withReplacedBody(r *http.Request, v any) *http.Request {
	body, _ := json.Marshal(v)
	r2 := r.Clone(r.Context())
	r2.Body = nopCloser{strings.NewReader(string(body))}
	r2.ContentLength = int64(len(body))
	r2.Header.Set("Content-Type", "application/json")
	return r2
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }

// firstWords returns up to limit characters from s, ending on a word
// boundary. Used to derive a conversation title from the first message.
func firstWords(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	if i := strings.LastIndexAny(cut, " \n"); i > limit/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
