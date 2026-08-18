package security

import "testing"

func TestValidateTopicPattern(t *testing.T) {
	ok := createPatternRequest{Pattern: "competitor", PatternType: "keyword", Action: "warn", Severity: "medium"}
	if err := validateTopicPattern(ok); err != nil {
		t.Fatalf("valid keyword: %v", err)
	}
	if err := validateTopicPattern(createPatternRequest{Pattern: "", PatternType: "keyword", Action: "warn", Severity: "medium"}); err == nil {
		t.Fatal("empty pattern must fail")
	}
	if err := validateTopicPattern(createPatternRequest{Pattern: "foo", PatternType: "glob", Action: "warn", Severity: "medium"}); err == nil {
		t.Fatal("unknown pattern_type must fail")
	}
	if err := validateTopicPattern(createPatternRequest{Pattern: "(", PatternType: "regex", Action: "block", Severity: "high"}); err == nil {
		t.Fatal("invalid regex must fail")
	}
}
