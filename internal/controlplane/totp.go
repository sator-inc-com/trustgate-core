package controlplane

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"time"
)

const (
	totpPeriod = 30 // seconds
	totpDigits = 6
	totpSkew   = 1 // allow ±1 time step for clock skew
)

// generateSecret creates a random 20-byte base32-encoded TOTP secret.
func generateSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// generateTOTP computes a 6-digit TOTP code for the given secret and time (RFC 6238).
func generateTOTP(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode TOTP secret: %w", err)
	}

	counter := uint64(t.Unix()) / uint64(totpPeriod)

	// Encode counter as big-endian 8-byte value
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	// HMAC-SHA1
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 Section 5.4)
	offset := h[len(h)-1] & 0x0f
	code := binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff

	// Modulo to get desired number of digits
	otp := code % uint32(math.Pow10(totpDigits))

	return fmt.Sprintf("%0*d", totpDigits, otp), nil
}

// validateTOTP checks if the provided code matches the expected TOTP,
// allowing ±1 time window (±30 seconds) for clock skew tolerance.
func validateTOTP(secret, code string) bool {
	now := time.Now()
	for i := -totpSkew; i <= totpSkew; i++ {
		t := now.Add(time.Duration(i*totpPeriod) * time.Second)
		expected, err := generateTOTP(secret, t)
		if err != nil {
			continue
		}
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

// totpURI generates an otpauth:// URI for QR code generation.
func totpURI(secret, issuer, account string) string {
	u := url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   fmt.Sprintf("/%s:%s", issuer, account),
	}
	q := u.Query()
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpPeriod))
	u.RawQuery = q.Encode()
	return u.String()
}
