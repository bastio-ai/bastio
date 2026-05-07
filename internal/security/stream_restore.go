package security

import "bytes"

// StreamRestorer replaces placeholders with their originals as bytes
// arrive in a stream. Placeholders can straddle SSE chunks, so the
// restorer keeps a small residual buffer until it's sure a trailing
// sequence is not mid-placeholder.
//
// The contract is simple:
//
//	r := security.NewStreamRestorer(tm)
//	for chunk := range chunks {
//	    out := r.Write(chunk)
//	    // out is safe to emit immediately; some bytes may be held
//	    // back in the residual until a later chunk resolves them.
//	}
//	tail := r.Flush()
//	// emit tail last
//
// A nil restorer, a nil TokenMap, or an empty map all short-circuit to
// pass-through so callers can use one code path for all configurations.
type StreamRestorer struct {
	tm       *TokenMap
	residual []byte
	style    TokenStyle
}

// maxStreamResidual caps how many bytes we hold before flushing some of
// them even if the tail *might* be a placeholder. Adversarial input (a
// long run of '<' characters) could otherwise consume unbounded memory.
// Longest legitimate placeholder is ~22 bytes, so 1KiB is safe overkill.
const maxStreamResidual = 1024

// streamSafetyTail is the number of bytes to always hold when forcing a
// flush under pressure. Must be ≥ longest possible placeholder so we
// never emit a partial placeholder that a later chunk would complete.
const streamSafetyTail = 32

// NewStreamRestorer returns a restorer bound to tm. A nil tm produces a
// pass-through restorer that never buffers.
func NewStreamRestorer(tm *TokenMap) *StreamRestorer {
	style := TokenStyleAngle
	if tm != nil {
		style = tm.Style()
	}
	return &StreamRestorer{tm: tm, style: style}
}

// Write appends chunk to the residual, then returns the prefix that is
// safe to emit after restoration. Bytes that might be mid-placeholder
// stay buffered until a later Write or Flush resolves them.
func (s *StreamRestorer) Write(chunk []byte) []byte {
	if s == nil || s.tm == nil || s.tm.Size() == 0 {
		return chunk
	}
	s.residual = append(s.residual, chunk...)

	trigger, closer := s.delimiters()
	lastTrigger := bytes.LastIndex(s.residual, trigger)

	if lastTrigger < 0 {
		// No potential placeholder start anywhere — emit everything.
		return s.flushAll()
	}

	tail := s.residual[lastTrigger:]
	if bytes.Contains(tail, closer) {
		// The trailing potential placeholder is closed. Whether or not
		// it's a real placeholder, Restore handles both cases (unknown
		// placeholders pass through).
		return s.flushAll()
	}

	// Residual holds an open, unresolved potential placeholder in its
	// tail. Emit everything before lastTrigger; keep the rest until
	// more bytes arrive.
	prefix := s.residual[:lastTrigger]
	out := s.restore(prefix)
	s.residual = append(s.residual[:0], s.residual[lastTrigger:]...)

	// Safety: if an adversarial stream keeps extending without closing,
	// bound the residual by forcing a partial flush that still retains
	// streamSafetyTail bytes (enough for the longest legitimate token).
	if len(s.residual) > maxStreamResidual {
		safe := len(s.residual) - streamSafetyTail
		extra := s.restore(s.residual[:safe])
		s.residual = append(s.residual[:0], s.residual[safe:]...)
		out = append(out, extra...)
	}

	return out
}

// Flush returns the remaining residual with restoration applied and
// resets the restorer. Call at end-of-stream.
func (s *StreamRestorer) Flush() []byte {
	if s == nil || len(s.residual) == 0 {
		return nil
	}
	out := s.restore(s.residual)
	s.residual = nil
	return out
}

func (s *StreamRestorer) flushAll() []byte {
	out := s.restore(s.residual)
	s.residual = nil
	return out
}

func (s *StreamRestorer) restore(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out, _ := s.tm.Restore(string(b))
	return []byte(out)
}

func (s *StreamRestorer) delimiters() (trigger, closer []byte) {
	if s.style == TokenStyleCurly {
		return []byte("{{"), []byte("}}")
	}
	return []byte("<"), []byte(">")
}
