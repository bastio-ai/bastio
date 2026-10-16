package detection

import (
	"regexp"
	"strings"
	"time"
)

func identifierDigits(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Hyphens separate groups within numeric identifiers as well as words. A
// regex word boundary is insufficient: 123-456-78-9012 contains a shape-valid
// 456-78-9012 suffix, but the complete token has twelve digits, not nine.
// Do not exclude URLs or field names: a complete SSN/SIN there still counts.
func wholeNumericIdentifier(content string, start, end int) bool {
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	if start >= 2 && content[start-1] == '-' && digit(content[start-2]) {
		return false
	}
	if end+1 < len(content) && content[end] == '-' && digit(content[end+1]) {
		return false
	}
	return true
}

// SSA: area 000, 666 and 900–999, group 00, and serial 0000 are invalid.
// https://secure.ssa.gov/poms.nsf/lnx/0110201035
func ssnValid(s string) bool {
	d := identifierDigits(s)
	return len(d) == 9 && d[:3] != "000" && d[:3] != "666" && d[0] != '9' && d[3:5] != "00" && d[5:] != "0000"
}

func cprDateValid(s string) bool {
	d := identifierDigits(s)
	if len(d) != 10 {
		return false
	}
	day, month, year := int(d[0]-'0')*10+int(d[1]-'0'), int(d[2]-'0')*10+int(d[3]-'0'), int(d[4]-'0')*10+int(d[5]-'0')
	// CPR's seventh digit determines the century together with YY.
	// No mod-11 requirement: valid assignments without it exist since 2007.
	// https://www.cpr.dk/media/12066/personnummeret-i-cpr.pdf
	century := 1900
	switch d[6] {
	case '4', '9':
		if year <= 36 {
			century = 2000
		}
	case '5', '6', '7', '8':
		century = 1800
		if year <= 57 {
			century = 2000
		}
	}
	date := time.Date(century+year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return date.Day() == day && int(date.Month()) == month
}

// HMRC explicitly excludes these otherwise shape-valid prefixes.
// https://www.gov.uk/hmrc-internal-manuals/national-insurance-manual/nim39110
func ninoValid(s string) bool {
	clean := strings.ToUpper(strings.Join(strings.Fields(s), ""))
	if len(clean) != 9 {
		return false
	}
	switch clean[:2] {
	case "BG", "GB", "KN", "NK", "NT", "TN", "ZZ":
		return false
	}
	return true
}

// Verhoeff's multiplication/permutation tables validate the checksum over
// all 12 digits. Checking shape alone also matches ordinary product IDs.
var verhoeffD = [10][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, {1, 2, 3, 4, 0, 6, 7, 8, 9, 5}, {2, 3, 4, 0, 1, 7, 8, 9, 5, 6}, {3, 4, 0, 1, 2, 8, 9, 5, 6, 7}, {4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
	{5, 9, 8, 7, 6, 0, 4, 3, 2, 1}, {6, 5, 9, 8, 7, 1, 0, 4, 3, 2}, {7, 6, 5, 9, 8, 2, 1, 0, 4, 3}, {8, 7, 6, 5, 9, 3, 2, 1, 0, 4}, {9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
}
var verhoeffP = [8][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, {1, 5, 7, 6, 2, 8, 3, 0, 9, 4}, {5, 8, 0, 3, 7, 9, 6, 1, 4, 2}, {8, 9, 1, 6, 0, 4, 3, 5, 2, 7}, {9, 4, 5, 3, 1, 2, 6, 8, 7, 0}, {4, 2, 8, 6, 5, 7, 3, 9, 0, 1}, {2, 7, 9, 3, 8, 0, 6, 4, 1, 5}, {7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
}

// UIDAI requires a 12-digit number conforming to the Verhoeff algorithm.
// https://uidai.gov.in/en/ecosystem/authentication-devices-documents/developer-section.html
func aadhaarValid(s string) bool {
	d := identifierDigits(s)
	if len(d) != 12 || d[0] < '2' {
		return false
	}
	c := 0
	for i := range len(d) {
		c = verhoeffD[c][verhoeffP[i%8][int(d[len(d)-1-i]-'0')]]
	}
	return c == 0
}

// National VAT formats (structure, not a claim that a registration exists).
// No live VIES lookups: identifiers must not be sent to another service.
// https://euipo.europa.eu/tunnel-web/secure/webdav/guest/document_library/Documents/COSME/VAT%20numbers%20EU.pdf
var vatFormats = map[string]*regexp.Regexp{}

func init() {
	for country, shape := range map[string]string{
		"AT": `U\d{8}`, "BE": `[01]\d{9}`, "BG": `\d{9,10}`, "CY": `\d{8}[A-Z]`, "CZ": `\d{8,10}`, "DE": `\d{9}`, "DK": `\d{8}`, "EE": `\d{9}`, "EL": `\d{9}`, "ES": `(?:[A-Z]\d{7}[A-Z0-9]|\d{8}[A-Z])`, "FI": `\d{8}`, "FR": `[A-Z0-9]{2}\d{9}`, "GB": `(?:\d{9}(?:\d{3})?|GD\d{3}|HA\d{3})`, "HR": `\d{11}`, "HU": `\d{8}`, "IE": `(?:\d{7}[A-Z]{1,2}|\d[A-Z*+]\d{5}[A-Z])`, "IT": `\d{11}`, "LT": `(?:\d{9}|\d{12})`, "LU": `\d{8}`, "LV": `\d{11}`, "MT": `\d{8}`, "NL": `\d{9}B\d{2}`, "PL": `\d{10}`, "PT": `\d{9}`, "RO": `\d{2,10}`, "SE": `\d{12}`, "SI": `\d{8}`, "SK": `\d{10}`, "XI": `(?:\d{9}(?:\d{3})?|GD\d{3}|HA\d{3})`,
	} {
		vatFormats[country] = regexp.MustCompile("^(?:" + shape + ")$")
	}
}
func vatValid(s string) bool {
	clean := strings.ToUpper(strings.Join(strings.Fields(s), ""))
	if len(clean) < 3 {
		return false
	}
	format := vatFormats[clean[:2]]
	return format != nil && format.MatchString(clean[2:])
}
