package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName        = "trustgate_session"
	tokenVerifiedCookieName  = "trustgate_token_verified"
	sessionTTL               = 24 * time.Hour
	tokenVerifiedTTL         = 5 * time.Minute
)

type sessionData struct {
	token     string
	username  string
	createdAt time.Time
}

// sessionStore is an in-memory store for UI sessions.
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionData
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		sessions: make(map[string]sessionData),
	}
}

// create generates a new session token for a given username, stores it, and returns it.
func (ss *sessionStore) create(username string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.sessions[token] = sessionData{
		token:     token,
		username:  username,
		createdAt: time.Now(),
	}
	return token, nil
}

// getUsername returns the username associated with a session token, or empty string if not found.
func (ss *sessionStore) getUsername(token string) string {
	if token == "" {
		return ""
	}
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	sd, ok := ss.sessions[token]
	if !ok {
		return ""
	}
	return sd.username
}

// validate checks whether a session token exists and is not expired.
// It also cleans up expired sessions encountered during lookup.
func (ss *sessionStore) validate(token string) bool {
	if token == "" {
		return false
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()

	sd, ok := ss.sessions[token]
	if !ok {
		return false
	}
	if time.Since(sd.createdAt) > sessionTTL {
		delete(ss.sessions, token)
		return false
	}
	return true
}

// delete removes a session.
func (ss *sessionStore) delete(token string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.sessions, token)
}

// cleanup removes all expired sessions.
func (ss *sessionStore) cleanup() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now()
	for k, v := range ss.sessions {
		if now.Sub(v.createdAt) > sessionTTL {
			delete(ss.sessions, k)
		}
	}
}

// sessionAuth returns middleware that validates UI session cookies.
// On failure, redirects to the login page.
func sessionAuth(ss *sessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Clean up expired sessions periodically
			ss.cleanup()

			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || !ss.validate(cookie.Value) {
				http.Redirect(w, r, "/ui/login", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setSessionCookie writes the session cookie to the response.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/ui/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/ui/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// createTokenVerified generates a short-lived token indicating password was verified.
// Stores the username so MFA handlers know which admin is authenticating.
func (ss *sessionStore) createTokenVerified(username string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.sessions[token] = sessionData{
		token:     token,
		username:  username,
		createdAt: time.Now(),
	}
	return token, nil
}

// validateTokenVerified checks whether a token_verified token exists and is within the 5-minute TTL.
func (ss *sessionStore) validateTokenVerified(token string) bool {
	if token == "" {
		return false
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()

	sd, ok := ss.sessions[token]
	if !ok {
		return false
	}
	if time.Since(sd.createdAt) > tokenVerifiedTTL {
		delete(ss.sessions, token)
		return false
	}
	return true
}

// setTokenVerifiedCookie writes the token_verified cookie to the response.
func setTokenVerifiedCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenVerifiedCookieName,
		Value:    token,
		Path:     "/ui/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(tokenVerifiedTTL.Seconds()),
	})
}

// clearTokenVerifiedCookie removes the token_verified cookie.
func clearTokenVerifiedCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     tokenVerifiedCookieName,
		Value:    "",
		Path:     "/ui/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
