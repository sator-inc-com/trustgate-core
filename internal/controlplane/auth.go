package controlplane

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// hashToken returns the SHA256 hex digest of a token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// adminApiAuth returns middleware that validates admin API tokens (tg_admin_*).
// Looks up the SHA256 hash of the bearer token in the admin_api_tokens table.
// Updates last_used on successful validation.
func adminApiAuth(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := extractBearer(r)
			if bearer == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenHash := hashToken(bearer)
			token, err := store.GetAdminAPIToken(tokenHash)
			if err != nil || token == nil {
				http.Error(w, `{"error":"invalid admin API token"}`, http.StatusForbidden)
				return
			}

			// Update last_used (best-effort, don't fail the request)
			_ = store.UpdateAdminAPITokenLastUsed(tokenHash)

			next.ServeHTTP(w, r)
		})
	}
}

// agentAuth returns middleware that validates agent_token + agent_id.
func agentAuth(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := extractBearer(r)
			if bearer == "" {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}
			agentID := r.Header.Get("X-Agent-ID")
			if agentID == "" {
				http.Error(w, `{"error":"missing X-Agent-ID header"}`, http.StatusBadRequest)
				return
			}

			// Look up agent and verify token hash
			agent, err := store.GetAgent(agentID)
			if err != nil {
				http.Error(w, `{"error":"agent not found"}`, http.StatusForbidden)
				return
			}
			if subtle.ConstantTimeCompare([]byte(hashToken(bearer)), []byte(agent.TokenHash)) != 1 {
				http.Error(w, `{"error":"invalid agent token"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// apiKeyAuth returns middleware that validates either an org API key or a department API key
// (X-API-Key header). Used for agent auto-registration endpoint.
// Org keys are validated against the configured org key.
// Department keys (tg_dept_*) are looked up in the store.
// On success, the validated key is stored in context for downstream handlers.
func apiKeyAuth(orgApiKey string, store *Store) func(http.Handler) http.Handler {
	orgKeyHash := hashToken(orgApiKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				http.Error(w, `{"error":"missing X-API-Key header"}`, http.StatusUnauthorized)
				return
			}

			// Check if it's a department key
			if strings.HasPrefix(key, "tg_dept_") {
				dept, err := store.GetDepartmentByApiKey(key)
				if err != nil || dept == nil {
					http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
					return
				}
				// Store department ID in header for downstream handler
				r.Header.Set("X-TrustGate-Dept-ID", dept.ID)
				next.ServeHTTP(w, r)
				return
			}

			// Org key validation
			if subtle.ConstantTimeCompare([]byte(hashToken(key)), []byte(orgKeyHash)) != 1 {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
