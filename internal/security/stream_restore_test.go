package security

import (
	"strings"
	"testing"
)

func TestStreamRestorer_PassThroughWithoutMap(t *testing.T) {
	r := NewStreamRestorer(nil)
	out := r.Write([]byte("hello <PII_SSN_1> world"))
	if string(out) != "hello <PII_SSN_1> world" {
		t.Errorf("pass-through mismatch: %q", out)
	}
	if tail := r.Flush(); tail != nil {
		t.Errorf("expected nil flush for nil map, got %q", tail)
	}
}

func TestStreamRestorer_WholePlaceholderInOneChunk(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	out := r.Write([]byte("Your SSN <PII_SSN_1> is safe."))
	if string(out) != "Your SSN 123-45-6789 is safe." {
		t.Errorf("expected restored output in one chunk, got %q", out)
	}
	if tail := r.Flush(); len(tail) != 0 {
		t.Errorf("expected empty flush, got %q", tail)
	}
}

func TestStreamRestorer_SplitPlaceholder(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	// Split "<PII_SSN_1>" across three writes.
	emitted := string(r.Write([]byte("Your SSN ")))
	emitted += string(r.Write([]byte("<PII_SSN")))
	emitted += string(r.Write([]byte("_1> is safe.")))
	emitted += string(r.Flush())

	if emitted != "Your SSN 123-45-6789 is safe." {
		t.Errorf("split restore failed: %q", emitted)
	}
}

func TestStreamRestorer_ByteByByteFragmentation(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")
	tm.Add("email", "alice@example.com")

	input := "Hi — your SSN <PII_SSN_1> and email <PII_EMAIL_1> look good."

	r := NewStreamRestorer(tm)
	var out strings.Builder
	// Iterate by byte (not rune) so multibyte runes like em-dash split
	// across Write calls — this is the worst-case fragmentation we
	// promise to handle.
	for i := 0; i < len(input); i++ {
		out.Write(r.Write([]byte{input[i]}))
	}
	out.Write(r.Flush())

	want := "Hi — your SSN 123-45-6789 and email alice@example.com look good."
	if out.String() != want {
		t.Errorf("byte-by-byte stream failed\n  want %q\n  got  %q", want, out.String())
	}
}

func TestStreamRestorer_CurlyStyle(t *testing.T) {
	tm := NewTokenMap(TokenStyleCurly)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	emitted := string(r.Write([]byte("Your SSN {{PII_")))
	emitted += string(r.Write([]byte("SSN_1}} is safe.")))
	emitted += string(r.Flush())

	if emitted != "Your SSN 123-45-6789 is safe." {
		t.Errorf("curly-style split failed: %q", emitted)
	}
}

func TestStreamRestorer_UnknownPlaceholderPassesThrough(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	out := string(r.Write([]byte("Unknown <PII_SSN_9> arrives intact"))) + string(r.Flush())

	if !strings.Contains(out, "<PII_SSN_9>") {
		t.Errorf("unknown placeholder stripped: %q", out)
	}
}

func TestStreamRestorer_AdversarialNoClose(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	// Feed a lot of '<' with no closing '>'. The restorer must not
	// balloon its residual past the safety cap.
	pad := strings.Repeat("<", maxStreamResidual*2)
	out := string(r.Write([]byte(pad)))
	out += string(r.Flush())

	if out != pad {
		t.Errorf("adversarial input not passed through cleanly: got %d bytes, want %d", len(out), len(pad))
	}
}

func TestStreamRestorer_HTMLLikeContentIsSafe(t *testing.T) {
	tm := NewTokenMap(TokenStyleAngle)
	tm.Add("ssn", "123-45-6789")

	r := NewStreamRestorer(tm)
	in := "See <strong>this</strong> and <PII_SSN_1> closely"
	out := string(r.Write([]byte(in))) + string(r.Flush())

	want := "See <strong>this</strong> and 123-45-6789 closely"
	if out != want {
		t.Errorf("html-like content mangled\n  want %q\n  got  %q", want, out)
	}
}
