package identity

import (
	"net/http"
	"testing"

	"github.com/trustgate/trustgate/internal/config"
)

func TestResolver_HeaderMode(t *testing.T) {
	r := NewResolver(config.IdentityConfig{
		Mode: "header",
		Headers: map[string]string{
			"user_id":    "X-TrustGate-User",
			"role":       "X-TrustGate-Role",
			"department": "X-TrustGate-Department",
			"clearance":  "X-TrustGate-Clearance",
		},
		OnMissing:     "anonymous",
		AnonymousRole: "guest",
	})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("X-TrustGate-User", "yamada")
	req.Header.Set("X-TrustGate-Role", "analyst")
	req.Header.Set("X-TrustGate-Department", "sales")
	req.Header.Set("X-TrustGate-Clearance", "internal")

	id := r.Resolve(req)

	if id.UserID != "yamada" {
		t.Errorf("UserID = %q, want yamada", id.UserID)
	}
	if id.Role != "analyst" {
		t.Errorf("Role = %q, want analyst", id.Role)
	}
	if id.Department != "sales" {
		t.Errorf("Department = %q, want sales", id.Department)
	}
	if id.Clearance != "internal" {
		t.Errorf("Clearance = %q, want internal", id.Clearance)
	}
	if id.AuthMethod != "header" {
		t.Errorf("AuthMethod = %q, want header", id.AuthMethod)
	}
}

func TestResolver_Anonymous(t *testing.T) {
	r := NewResolver(config.IdentityConfig{
		Mode: "header",
		Headers: map[string]string{
			"user_id": "X-TrustGate-User",
		},
		OnMissing:     "anonymous",
		AnonymousRole: "guest",
	})

	req, _ := http.NewRequest("POST", "/", nil)
	// No headers set

	id := r.Resolve(req)

	if id.UserID != "anonymous" {
		t.Errorf("UserID = %q, want anonymous", id.UserID)
	}
	if id.Role != "guest" {
		t.Errorf("Role = %q, want guest", id.Role)
	}
	if id.AuthMethod != "anonymous" {
		t.Errorf("AuthMethod = %q, want anonymous", id.AuthMethod)
	}
}

func TestResolver_BlockOnMissing(t *testing.T) {
	r := NewResolver(config.IdentityConfig{
		Mode: "header",
		Headers: map[string]string{
			"user_id": "X-TrustGate-User",
		},
		OnMissing: "block",
	})

	req, _ := http.NewRequest("POST", "/", nil)
	id := r.Resolve(req)

	if id.UserID != "" {
		t.Errorf("UserID = %q, want empty (block mode)", id.UserID)
	}
	if id.AuthMethod != "none" {
		t.Errorf("AuthMethod = %q, want none", id.AuthMethod)
	}
}
