package detection

import (
	"context"
	"strings"
	"testing"
)

func TestPIIDetector_Detect(t *testing.T) {
	d := NewPIIDetector()

	tests := []struct {
		name      string
		content   string
		wantTypes []string
		wantCount int
	}{
		{
			name:      "no PII",
			content:   "Hello, how are you today?",
			wantCount: 0,
		},
		{
			name:      "email address",
			content:   "Contact me at john@example.com please",
			wantTypes: []string{"email"},
			wantCount: 1,
		},
		{
			name:      "SSN",
			content:   "My social security number is 123-45-6789",
			wantTypes: []string{"ssn"},
			wantCount: 1,
		},
		{
			name:      "SSN bare 9 digits",
			content:   "my ssn is 123456789",
			wantTypes: []string{"ssn"},
			wantCount: 1,
		},
		{
			name:      "SSN space separated",
			content:   "SSN: 123 45 6789",
			wantTypes: []string{"ssn"},
			wantCount: 1,
		},
		{
			name:      "phone number",
			content:   "Call me at (555) 123-4567",
			wantTypes: []string{"phone"},
			wantCount: 1,
		},
		{
			name:      "multiple PII types",
			content:   "Email john@test.com, SSN 123-45-6789, call 555-123-4567",
			wantCount: 3,
		},
		{
			name:      "IP address",
			content:   "Server is at 192.168.1.100",
			wantTypes: []string{"ip_address"},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, err := d.Detect(context.Background(), tt.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(findings) != tt.wantCount {
				t.Errorf("expected %d findings, got %d", tt.wantCount, len(findings))
				for _, f := range findings {
					t.Logf("  found: %s (%s)", f.MatchedPattern, f.MatchedContent)
				}
			}

			if tt.wantTypes != nil && len(findings) > 0 {
				for _, wantType := range tt.wantTypes {
					found := false
					for _, f := range findings {
						if f.MatchedPattern == wantType {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected PII type %s not found", wantType)
					}
				}
			}
		})
	}
}

func TestPIIDetector_Redact(t *testing.T) {
	d := NewPIIDetector()

	tests := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{
			name:  "redact SSN",
			input: "SSN is 123-45-6789",
			check: func(s string) bool { return strings.Contains(s, "***-**-6789") && !strings.Contains(s, "123-45") },
		},
		{
			name:  "redact email",
			input: "Email john@example.com",
			check: func(s string) bool { return strings.Contains(s, "j***@example.com") && !strings.Contains(s, "john@") },
		},
		{
			name:  "redact phone",
			input: "Call 555-123-4567",
			check: func(s string) bool { return strings.Contains(s, "***-***-4567") && !strings.Contains(s, "555-123") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := d.Redact(tt.input)
			if !tt.check(result) {
				t.Errorf("redaction failed: %q -> %q", tt.input, result)
			}
		})
	}
}

func TestPIIDetector_LuhnFiltersInvalidCreditCards(t *testing.T) {
	d := NewPIIDetector()
	// Real-looking card with a bad last digit — passes shape, fails Luhn.
	findings, _ := d.Detect(context.Background(), "Order id: 4111 1111 1111 1112")
	for _, f := range findings {
		if f.MatchedPattern == "credit_card" {
			t.Errorf("Luhn-invalid number should not fire credit_card: %+v", f)
		}
	}
	// Real Visa test card, passes Luhn.
	real, _ := d.Detect(context.Background(), "Pay with 4111 1111 1111 1111")
	found := false
	for _, f := range real {
		if f.MatchedPattern == "credit_card" {
			found = true
		}
	}
	if !found {
		t.Error("Luhn-valid card should fire credit_card")
	}
}

func TestPIIDetector_IBAN(t *testing.T) {
	d := NewPIIDetector()
	// Real-looking valid German IBAN.
	findings, _ := d.Detect(context.Background(), "IBAN: DE89 3704 0044 0532 0130 00")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "iban" {
			found = true
		}
	}
	if !found {
		t.Error("expected iban finding on valid IBAN")
	}
}

func TestPIIDetector_UKNINO(t *testing.T) {
	d := NewPIIDetector()
	findings, _ := d.Detect(context.Background(), "NI number AB 12 34 56 C")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "uk_nino" {
			found = true
		}
	}
	if !found {
		t.Error("expected uk_nino finding")
	}
}

func TestPIIDetector_EUVAT(t *testing.T) {
	d := NewPIIDetector()
	findings, _ := d.Detect(context.Background(), "Invoice for DE123456789")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "eu_vat" {
			found = true
		}
	}
	if !found {
		t.Error("expected eu_vat finding")
	}
}

func TestPIIDetector_DanishCPR(t *testing.T) {
	d := NewPIIDetector()
	findings, _ := d.Detect(context.Background(), "CPR 190987-2231 on the form")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "danish_cpr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected danish_cpr, got %+v", findings)
	}
	spaced, _ := d.Detect(context.Background(), "personnummer 19 09 87 2231")
	found = false
	for _, f := range spaced {
		if f.MatchedPattern == "danish_cpr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected spaced danish_cpr, got %+v", spaced)
	}
	// Hyphenless 10-digit runs are intentionally not matched.
	bare, _ := d.Detect(context.Background(), "order 1909872231 shipped")
	for _, f := range bare {
		if f.MatchedPattern == "danish_cpr" {
			t.Errorf("hyphenless CPR should not fire: %+v", f)
		}
	}
}
