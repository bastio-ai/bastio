package detection

import (
	"context"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

func TestSSNRejectsFragmentsOfLongerHyphenatedIdentifiers(t *testing.T) {
	d := NewPIIDetector()
	for _, text := range []string{
		"123-456-78-9012",
		"123-45-6789-012",
		"123-456789012",
		"123456789-012",
		"https://cdn.example.test/s/files/1/0123/4567/8901/files/123-456-78-9012.jpg?v=1763400017",
		"https://shop.example.test/images/123-45-6789-012.jpg",
		"Product reference: 123-456-78-9012. Price 249 DKK.",
	} {
		t.Run(text, func(t *testing.T) {
			findings, err := d.Detect(context.Background(), text)
			if err != nil || len(findings) != 0 {
				t.Fatalf("numeric identifier fragment classified as personal data: %+v %v", findings, err)
			}
			if d.Mask(text) != text {
				t.Fatal("public identifier changed by masking")
			}
			tokenized, tokens := d.Tokenize(text, security.TokenStyleAngle)
			if tokenized != text || tokens.Size() != 0 {
				t.Fatal("public identifier changed by tokenization")
			}
		})
	}
}

func TestCompleteSSNsRemainProtectedAlongsideProductIdentifiers(t *testing.T) {
	d := NewPIIDetector()
	for _, ssn := range []string{"123-45-6789", "123 45 6789", "123456789"} {
		for _, text := range []string{
			"SSN: " + ssn,
			"https://example.test/search?ssn=" + ssn,
			"https://example.test/images/" + ssn + ".jpg",
			"Product 123-456-78-9012; SSN: " + ssn,
			"SSN: " + ssn + ". Product 123-456-78-9012",
		} {
			findings, err := d.Detect(context.Background(), text)
			if err != nil || len(findings) != 1 || findings[0].MatchedPattern != "ssn" {
				t.Fatalf("complete SSN was not detected: %+v %v", findings, err)
			}
			if strings.Contains(d.Mask(text), ssn) {
				t.Fatal("complete SSN was not masked")
			}
			tokenized, tokens := d.Tokenize(text, security.TokenStyleAngle)
			if strings.Contains(tokenized, ssn) || tokens.Size() != 1 {
				t.Fatal("complete SSN was not tokenized")
			}
			restored, _ := tokens.Restore(tokenized)
			if restored != text {
				t.Fatal("SSN token restoration changed the payload")
			}
		}
	}
}

func TestSINRejectsLongerIdentifiersWithoutLosingCompleteNumbers(t *testing.T) {
	d := NewPIIDetector()
	// Generate a valid checksum fixture; the leading 9 avoids SSN overlap.
	var sin string
	for digit := '0'; digit <= '9'; digit++ {
		candidate := "98765432" + string(digit)
		if luhnValidLength(candidate, 9, 9) {
			sin = candidate
			break
		}
	}
	if sin == "" {
		t.Fatal("no valid SIN fixture")
	}
	for _, value := range []string{sin, sin[:3] + "-" + sin[3:6] + "-" + sin[6:]} {
		for _, text := range []string{"123-" + value, value + "-123", "https://example.test/files/123-" + value + ".jpg"} {
			findings, err := d.Detect(context.Background(), text)
			if err != nil || len(findings) != 0 || d.Mask(text) != text {
				t.Fatalf("partial SIN in longer identifier accepted: %+v %v", findings, err)
			}
			tokenized, tokens := d.Tokenize(text, security.TokenStyleAngle)
			if tokenized != text || tokens.Size() != 0 {
				t.Fatal("partial SIN tokenized")
			}
		}
		for _, text := range []string{"SIN: " + value, "https://example.test/files/" + value + ".jpg"} {
			findings, err := d.Detect(context.Background(), text)
			if err != nil || len(findings) != 1 || findings[0].MatchedPattern != "canadian_sin" || d.Mask(text) == text {
				t.Fatalf("complete SIN not protected: %+v %v", findings, err)
			}
			tokenized, tokens := d.Tokenize(text, security.TokenStyleAngle)
			restored, _ := tokens.Restore(tokenized)
			if tokenized == text || restored != text {
				t.Fatal("complete SIN tokenization failed")
			}
		}
	}
}
