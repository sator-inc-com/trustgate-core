package identity

import (
	"net/http"

	"github.com/trustgate/trustgate/internal/config"
)

// Identity represents the resolved user identity.
type Identity struct {
	UserID     string            `json:"user_id"`
	Role       string            `json:"role"`
	Department string            `json:"department"`
	Clearance  string            `json:"clearance"`
	AuthMethod string            `json:"auth_method"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Resolver resolves identity from HTTP requests.
type Resolver struct {
	cfg config.IdentityConfig
}

func NewResolver(cfg config.IdentityConfig) *Resolver {
	return &Resolver{cfg: cfg}
}

// Resolve extracts identity from the request based on configured mode.
func (r *Resolver) Resolve(req *http.Request) Identity {
	switch r.cfg.Mode {
	case "header":
		return r.resolveFromHeaders(req)
	default:
		return r.resolveFromHeaders(req)
	}
}

func (r *Resolver) resolveFromHeaders(req *http.Request) Identity {
	id := Identity{
		AuthMethod: "header",
		Attributes: make(map[string]string),
	}

	for field, header := range r.cfg.Headers {
		val := req.Header.Get(header)
		if val == "" {
			continue
		}
		id.Attributes[field] = val
		switch field {
		case "user_id":
			id.UserID = val
		case "role":
			id.Role = val
		case "department":
			id.Department = val
		case "clearance":
			id.Clearance = val
		}
	}

	if id.UserID == "" {
		switch r.cfg.OnMissing {
		case "anonymous":
			id.UserID = "anonymous"
			id.Role = r.cfg.AnonymousRole
			id.AuthMethod = "anonymous"
		case "block":
			id.UserID = ""
			id.AuthMethod = "none"
		default: // allow
			id.UserID = "unknown"
			id.AuthMethod = "none"
		}
	}

	return id
}
