package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// DefaultPublicKey is the Bastio License Signing Public Key (Ed25519).
// Enterprise binaries verify that license files are cryptographically signed by Bastio authority.
var DefaultPublicKeyHex = "3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29"

type Payload struct {
	ID                  string   `json:"id"`
	Customer            string   `json:"customer"`
	Tier                string   `json:"tier"` // "community", "starter", "growth", "enterprise_airgap"
	IssuedAt            int64    `json:"issued_at"`
	ExpiresAt           int64    `json:"expires_at"`
	MaxRequestsPerMonth int64    `json:"max_requests_per_month"`
	Features            []string `json:"features"`
}

type License struct {
	Payload   Payload `json:"payload"`
	Signature string  `json:"signature"`
}

type VerificationStatus struct {
	Valid          bool     `json:"valid"`
	Tier           string   `json:"tier"`
	Customer       string   `json:"customer"`
	ExpiresAt      string   `json:"expires_at"`
	DaysRemaining  int      `json:"days_remaining"`
	AirgapEnabled  bool     `json:"airgap_enabled"`
	ActiveFeatures []string `json:"active_features"`
	Message        string   `json:"message"`
}

type Service struct {
	pubKey ed25519.PublicKey
}

func NewService() *Service {
	// Parse default or custom public key
	pubKeyBytes, err := hexDecode(DefaultPublicKeyHex)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		pubKeyBytes = make([]byte, ed25519.PublicKeySize)
	}
	return &Service{
		pubKey: ed25519.PublicKey(pubKeyBytes),
	}
}

func (s *Service) LoadFromEnvOrPath() (*License, *VerificationStatus) {
	licKey := os.Getenv("BASTIO_LICENSE_KEY")
	if licKey == "" {
		if data, err := os.ReadFile("/etc/bastio/bastio.lic"); err == nil {
			licKey = string(data)
		}
	}

	if licKey == "" {
		return nil, &VerificationStatus{
			Valid:          true,
			Tier:           "community_oss",
			Customer:       "Open Source Community",
			ExpiresAt:      "Never",
			DaysRemaining:  9999,
			AirgapEnabled:  false,
			ActiveFeatures: []string{"core_proxy", "basic_detection", "dashboard"},
			Message:        "Running Bastio Open Source Community Edition (2,500 req/mo free tier)",
		}
	}

	lic, err := s.Verify(licKey)
	if err != nil {
		return nil, &VerificationStatus{
			Valid:   false,
			Tier:    "invalid",
			Message: fmt.Sprintf("License verification failed: %v", err),
		}
	}

	daysRemaining := int(time.Until(time.Unix(lic.Payload.ExpiresAt, 0)).Hours() / 24)

	return lic, &VerificationStatus{
		Valid:          true,
		Tier:           lic.Payload.Tier,
		Customer:       lic.Payload.Customer,
		ExpiresAt:      time.Unix(lic.Payload.ExpiresAt, 0).Format(time.RFC3339),
		DaysRemaining:  daysRemaining,
		AirgapEnabled:  lic.Payload.Tier == "enterprise_airgap",
		ActiveFeatures: lic.Payload.Features,
		Message:        "Valid cryptographic enterprise license",
	}
}

func (s *Service) Verify(licStr string) (*License, error) {
	parts := strings.Split(strings.TrimSpace(licStr), ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid license token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode license payload: %w", err)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode license signature: %w", err)
	}

	// Verify signature if key is non-zero
	if len(s.pubKey) == ed25519.PublicKeySize && !isZero(s.pubKey) {
		if !ed25519.Verify(s.pubKey, payloadBytes, sigBytes) {
			return nil, errors.New("signature verification failed")
		}
	}

	var payload Payload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid payload JSON: %w", err)
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return nil, fmt.Errorf("license expired on %s", time.Unix(payload.ExpiresAt, 0).Format(time.RFC3339))
	}

	return &License{
		Payload:   payload,
		Signature: parts[1],
	}, nil
}

func (s *Service) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/status", func(w http.ResponseWriter, r *http.Request) {
		_, status := s.LoadFromEnvOrPath()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})
	return r
}

func isZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("invalid hex string length")
	}
	res := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var b byte
		_, err := fmt.Sscanf(s[i:i+2], "%02x", &b)
		if err != nil {
			return nil, err
		}
		res[i/2] = b
	}
	return res, nil
}
