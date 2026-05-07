package workspace

import (
	"strings"
	"testing"
)

func TestChunkTextEmptyInput(t *testing.T) {
	t.Parallel()
	if got := chunkText(""); len(got) != 0 {
		t.Fatalf("expected zero chunks, got %d", len(got))
	}
}

func TestChunkTextShortInput(t *testing.T) {
	t.Parallel()
	got := chunkText("hello world")
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("expected single chunk, got %v", got)
	}
}

func TestChunkTextSplitsOnSentenceBoundary(t *testing.T) {
	t.Parallel()
	// 3000 chars of repeating sentences — should split at sentence ends,
	// not mid-word.
	sentence := "This is a sentence about cats and dogs. "
	body := strings.Repeat(sentence, 100) // ~4000 chars
	chunks := chunkText(body)
	if len(chunks) < 3 {
		t.Fatalf("expected ≥3 chunks for ~4000-char body, got %d", len(chunks))
	}
	for i, c := range chunks {
		// Either ends with sentence terminator or is the final chunk.
		if i == len(chunks)-1 {
			continue
		}
		last := c[len(c)-1]
		if last != '.' && last != '!' && last != '?' {
			t.Errorf("chunk %d doesn't end on sentence boundary: %q", i, c[len(c)-30:])
		}
	}
}

func TestKeywordTokensFiltersStopwords(t *testing.T) {
	t.Parallel()
	got := keywordTokens("What is the project status this week?")
	// "what", "the", "this" are stopwords; remaining tokens of length ≥3.
	expectedSet := map[string]bool{"project": true, "status": true, "week": true}
	for _, tok := range got {
		if !expectedSet[tok] {
			t.Errorf("unexpected token %q", tok)
		}
	}
	if len(got) > 5 {
		t.Errorf("token list should cap at 5, got %d", len(got))
	}
}

func TestKeywordTokensDedup(t *testing.T) {
	t.Parallel()
	got := keywordTokens("project project project plan plan goal")
	// project and plan dedup; goal kept; "the" stopword.
	if len(got) != 3 {
		t.Fatalf("dedup failed: %v", got)
	}
}
