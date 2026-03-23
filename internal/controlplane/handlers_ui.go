package controlplane

import (
	cryptoRandPkg "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/trustgate/trustgate/internal/controlplane/ui"
	"golang.org/x/crypto/bcrypt"
)

var cryptoRand io.Reader = cryptoRandPkg.Reader

var templates *ui.Templates

func init() {
	var err error
	templates, err = ui.NewTemplates()
	if err != nil {
		panic("failed to parse UI templates: " + err.Error())
	}
}

type pageData struct {
	Title string
	Page  string
}

func (s *Server) handleUIDashboard(w http.ResponseWriter, r *http.Request) {
	days := 7
	kpi, err := s.store.GetKPISummary(days)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get KPI summary for dashboard")
		kpi = &KPISummary{}
	}

	daily, err := s.store.GetDailyActionCounts(days)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get daily action counts")
	}

	dailyJSON, _ := json.Marshal(daily)
	if daily == nil {
		dailyJSON = []byte("[]")
	}

	// Build server URL for quick start guide
	serverHost := s.cfg.Listen.Host
	if serverHost == "0.0.0.0" || serverHost == "" {
		serverHost = "localhost"
	}
	serverURL := fmt.Sprintf("http://%s:%d", serverHost, s.cfg.Listen.Port)

	data := struct {
		pageData
		KPI       *KPISummary
		Days      int
		DailyJSON string
		ApiKey    string
		ServerURL string
	}{
		pageData:  pageData{Title: "Dashboard", Page: "dashboard"},
		KPI:       kpi,
		Days:      days,
		DailyJSON: string(dailyJSON),
		ApiKey:    s.cfg.Auth.ApiKey,
		ServerURL: serverURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render dashboard")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// AgentView is an Agent with resolved department name for display.
type AgentView struct {
	Agent
	DepartmentName string
}

func (s *Server) handleUIAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list agents for UI")
		agents = []Agent{}
	}
	if agents == nil {
		agents = []Agent{}
	}

	// Resolve department names
	deptMap, _ := s.store.GetDepartmentMap()
	var views []AgentView
	for _, a := range agents {
		v := AgentView{Agent: a}
		// Extract department from labels JSON
		if a.Labels != "" {
			var labels map[string]string
			if err := json.Unmarshal([]byte(a.Labels), &labels); err == nil {
				if deptID, ok := labels["department"]; ok {
					if name, found := deptMap[deptID]; found {
						v.DepartmentName = name
					} else {
						v.DepartmentName = deptID
					}
				}
			}
		}
		views = append(views, v)
	}
	if views == nil {
		views = []AgentView{}
	}

	data := struct {
		pageData
		Agents []AgentView
	}{
		pageData: pageData{Title: "Agents", Page: "agents"},
		Agents:   views,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render agents")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleUIAgentDelete deletes an agent from the registry.
func (s *Server) handleUIAgentDelete(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, `{"error":"missing agent ID"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteAgent(agentID); err != nil {
		s.logger.Error().Err(err).Str("agent_id", agentID).Msg("failed to delete agent")
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	s.logger.Info().Str("agent_id", agentID).Msg("agent deleted")
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"deleted"}`))
}

func (s *Server) handleUIPolicies(w http.ResponseWriter, r *http.Request) {
	// Check for scope filter from query param
	scopeFilter := r.URL.Query().Get("scope")

	currentVersion, policies, err := s.store.GetLatestPolicies()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get policies for UI")
	}

	// Apply scope filter if provided
	if scopeFilter != "" && scopeFilter != "all" && currentVersion > 0 {
		policies, err = s.store.GetPoliciesByVersionAndScope(currentVersion, scopeFilter)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to get filtered policies for UI")
		}
	}

	if policies == nil {
		policies = []PolicyRecord{}
	}

	versions, err := s.store.ListPolicyVersions()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list policy versions for UI")
	}

	// Load departments for the scope filter dropdown
	departments, err := s.store.ListDepartments()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list departments for policy UI")
	}
	if departments == nil {
		departments = []Department{}
	}

	data := struct {
		pageData
		CurrentVersion int
		NextVersion    int
		Policies       []PolicyRecord
		Versions       []PolicyVersion
		Departments    []Department
		ScopeFilter    string
	}{
		pageData:       pageData{Title: "Policies", Page: "policies"},
		CurrentVersion: currentVersion,
		NextVersion:    currentVersion + 1,
		Policies:       policies,
		Versions:       versions,
		Departments:    departments,
		ScopeFilter:    scopeFilter,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render policies")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleUIReports(w http.ResponseWriter, r *http.Request) {
	days := 30
	kpi, err := s.store.GetKPISummary(days)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get KPI summary for reports")
		kpi = &KPISummary{}
	}

	daily, err := s.store.GetDailyActionCounts(7)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get daily action counts for reports")
	}

	topViolations, err := s.store.GetTopPolicyViolations(days, 10)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get top policy violations")
	}
	if topViolations == nil {
		topViolations = []TopPolicyViolation{}
	}

	agentActivity, err := s.store.GetAgentActivity(days)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get agent activity")
	}
	if agentActivity == nil {
		agentActivity = []AgentActivity{}
	}

	violationsToday, err := s.store.GetViolationsToday()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get violations today")
	}

	// Build daily chart data: group by date with per-action counts
	type DayBar struct {
		Date       string
		AllowCount int
		WarnCount  int
		BlockCount int
		Total      int
	}
	dayMap := make(map[string]*DayBar)
	var maxTotal int
	for _, d := range daily {
		bar, ok := dayMap[d.Date]
		if !ok {
			bar = &DayBar{Date: d.Date}
			dayMap[d.Date] = bar
		}
		switch d.Action {
		case "allow":
			bar.AllowCount += d.Count
		case "warn":
			bar.WarnCount += d.Count
		case "block":
			bar.BlockCount += d.Count
		}
		bar.Total = bar.AllowCount + bar.WarnCount + bar.BlockCount
		if bar.Total > maxTotal {
			maxTotal = bar.Total
		}
	}

	// Sort days
	var dayBars []DayBar
	for _, bar := range dayMap {
		dayBars = append(dayBars, *bar)
	}
	// Simple sort by date string
	for i := 0; i < len(dayBars); i++ {
		for j := i + 1; j < len(dayBars); j++ {
			if dayBars[i].Date > dayBars[j].Date {
				dayBars[i], dayBars[j] = dayBars[j], dayBars[i]
			}
		}
	}

	data := struct {
		pageData
		KPI             *KPISummary
		Days            int
		ViolationsToday int
		DayBars         []DayBar
		MaxTotal        int
		TopViolations   []TopPolicyViolation
		AgentActivity   []AgentActivity
	}{
		pageData:        pageData{Title: "Reports", Page: "reports"},
		KPI:             kpi,
		Days:            days,
		ViolationsToday: violationsToday,
		DayBars:         dayBars,
		MaxTotal:        maxTotal,
		TopViolations:   topViolations,
		AgentActivity:   agentActivity,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render reports")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleUIDepartments(w http.ResponseWriter, r *http.Request) {
	departments, err := s.store.ListDepartments()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list departments for UI")
		departments = []Department{}
	}
	if departments == nil {
		departments = []Department{}
	}

	data := struct {
		pageData
		Departments []Department
	}{
		pageData:    pageData{Title: "Departments", Page: "departments"},
		Departments: departments,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render departments")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleUIDepartmentsJSON returns departments as JSON for the dashboard department selector.
func (s *Server) handleUIDepartmentsJSON(w http.ResponseWriter, r *http.Request) {
	departments, err := s.store.ListDepartments()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list departments for UI JSON")
		departments = []Department{}
	}
	if departments == nil {
		departments = []Department{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(departments)
}

// handleAuthToken authenticates with username/password and returns a new admin API token.
// POST /api/v1/auth/token - no auth required (this IS the auth endpoint).
func (s *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"username and password are required"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "api-token"
	}

	// Authenticate admin
	admin, err := s.store.GetAdmin(req.Username)
	if err != nil || admin == nil {
		http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	// Generate token: tg_admin_ + 32 random hex chars
	tokenBytes := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand, tokenBytes); err != nil {
		s.logger.Error().Err(err).Msg("failed to generate token random bytes")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	rawToken := "tg_admin_" + fmt.Sprintf("%x", tokenBytes)

	// Store SHA256 hash
	tokenRecord := &AdminAPIToken{
		TokenHash: hashToken(rawToken),
		Username:  req.Username,
		Name:      req.Name,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateAdminAPIToken(tokenRecord); err != nil {
		s.logger.Error().Err(err).Msg("failed to store admin API token")
		http.Error(w, `{"error":"failed to create token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      rawToken,
		"name":       req.Name,
		"expires_in": 0,
	})
}

// handleUITokenCreate generates a new admin API token from the UI (session-authenticated).
func (s *Server) handleUITokenCreate(w http.ResponseWriter, r *http.Request) {
	// Get current username from session
	currentUser := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentUser = s.sessions.getUsername(cookie.Value)
	}
	if currentUser == "" {
		http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "api-token"
	}

	// Generate token: tg_admin_ + 32 random hex chars
	tokenBytes := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand, tokenBytes); err != nil {
		s.logger.Error().Err(err).Msg("failed to generate token random bytes")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	rawToken := "tg_admin_" + fmt.Sprintf("%x", tokenBytes)

	tokenRecord := &AdminAPIToken{
		TokenHash: hashToken(rawToken),
		Username:  currentUser,
		Name:      req.Name,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateAdminAPIToken(tokenRecord); err != nil {
		s.logger.Error().Err(err).Msg("failed to store admin API token")
		http.Error(w, `{"error":"failed to create token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": rawToken,
		"name":  req.Name,
	})
}

// handleUITokenRevoke revokes an admin API token.
func (s *Server) handleUITokenRevoke(w http.ResponseWriter, r *http.Request) {
	tokenHash := chi.URLParam(r, "hash")
	if tokenHash == "" {
		http.Error(w, `{"error":"missing token hash"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteAdminAPIToken(tokenHash); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// handleUILoginPage renders the login page.
func (s *Server) handleUILoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if cookie, err := r.Cookie(sessionCookieName); err == nil && s.sessions.validate(cookie.Value) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}

	data := struct {
		Error string
	}{}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RenderLogin(w, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render login")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleUILoginSubmit processes the login form submission using username/password.
func (s *Server) handleUILoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	renderError := func(msg string) {
		data := struct {
			Error string
		}{Error: msg}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if err := templates.RenderLogin(w, data); err != nil {
			s.logger.Error().Err(err).Msg("failed to render login with error")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}

	if username == "" || password == "" {
		renderError("Username and password are required.")
		return
	}

	admin, err := s.store.GetAdmin(username)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to look up admin")
		renderError("Invalid username or password.")
		return
	}
	if admin == nil {
		renderError("Invalid username or password.")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		renderError("Invalid username or password.")
		return
	}

	// Update last login
	_ = s.store.UpdateAdminLastLogin(username)

	// Check if MFA is required (global config)
	if !s.cfg.Auth.IsMFARequired() {
		sessionToken, err := s.sessions.create(username)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to create session")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, sessionToken)
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return
	}

	// MFA is required: set temporary token_verified cookie and redirect to MFA
	tvToken, err := s.sessions.createTokenVerified(username)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create token_verified session")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setTokenVerifiedCookie(w, tvToken)

	// Check per-user MFA status
	if admin.MFAEnabled && admin.MFASecret != "" {
		http.Redirect(w, r, "/ui/mfa/verify", http.StatusFound)
	} else {
		http.Redirect(w, r, "/ui/mfa/setup", http.StatusFound)
	}
}

// handleUILogout clears the session and redirects to login.
func (s *Server) handleUILogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.delete(cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

// requireTokenVerified checks the token_verified cookie is valid.
// Returns true if valid, false if invalid (and writes redirect response).
func (s *Server) requireTokenVerified(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie(tokenVerifiedCookieName)
	if err != nil || !s.sessions.validateTokenVerified(cookie.Value) {
		http.Redirect(w, r, "/ui/login", http.StatusFound)
		return false
	}
	return true
}

// getTokenVerifiedUsername returns the username from the token_verified cookie.
func (s *Server) getTokenVerifiedUsername(r *http.Request) string {
	cookie, err := r.Cookie(tokenVerifiedCookieName)
	if err != nil {
		return ""
	}
	return s.sessions.getUsername(cookie.Value)
}

// handleMFASetupPage shows the QR code and secret for MFA setup.
func (s *Server) handleMFASetupPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireTokenVerified(w, r) {
		return
	}

	username := s.getTokenVerifiedUsername(r)
	if username == "" {
		http.Redirect(w, r, "/ui/login", http.StatusFound)
		return
	}

	// Generate a new secret
	secret, err := generateSecret()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to generate TOTP secret")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Save the secret to the admin's record (not yet enabled)
	if err := s.store.UpdateAdminMFA(username, secret, false); err != nil {
		s.logger.Error().Err(err).Msg("failed to save TOTP secret for admin")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	uri := totpURI(secret, "TrustGate", username)

	data := struct {
		Secret  string
		TOTPURI string
		Error   string
	}{
		Secret:  secret,
		TOTPURI: uri,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RenderMFASetup(w, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render MFA setup")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleMFASetupSubmit validates the confirmation code and enables MFA.
func (s *Server) handleMFASetupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireTokenVerified(w, r) {
		return
	}

	username := s.getTokenVerifiedUsername(r)
	if username == "" {
		http.Redirect(w, r, "/ui/login", http.StatusFound)
		return
	}

	code := r.FormValue("code")

	// Get the admin's pending MFA secret
	admin, err := s.store.GetAdmin(username)
	if err != nil || admin == nil || admin.MFASecret == "" {
		s.logger.Error().Err(err).Msg("failed to get admin MFA secret during setup")
		http.Redirect(w, r, "/ui/mfa/setup", http.StatusFound)
		return
	}

	if !validateTOTP(admin.MFASecret, code) {
		uri := totpURI(admin.MFASecret, "TrustGate", username)
		data := struct {
			Secret  string
			TOTPURI string
			Error   string
		}{
			Secret:  admin.MFASecret,
			TOTPURI: uri,
			Error:   "Invalid code. Please try again.",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if err := templates.RenderMFASetup(w, data); err != nil {
			s.logger.Error().Err(err).Msg("failed to render MFA setup with error")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// Enable MFA for this admin
	if err := s.store.UpdateAdminMFA(username, admin.MFASecret, true); err != nil {
		s.logger.Error().Err(err).Msg("failed to enable MFA for admin")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Clear token_verified and create full session
	cookie, _ := r.Cookie(tokenVerifiedCookieName)
	if cookie != nil {
		s.sessions.delete(cookie.Value)
	}
	clearTokenVerifiedCookie(w)

	sessionToken, err := s.sessions.create(username)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create session after MFA setup")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sessionToken)
	http.Redirect(w, r, "/ui/", http.StatusFound)
}

// handleMFAVerifyPage shows the TOTP code input form.
func (s *Server) handleMFAVerifyPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireTokenVerified(w, r) {
		return
	}

	data := struct {
		Error string
	}{}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.RenderMFAVerify(w, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render MFA verify")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleMFAVerifySubmit validates the TOTP code and creates a full session.
func (s *Server) handleMFAVerifySubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireTokenVerified(w, r) {
		return
	}

	username := s.getTokenVerifiedUsername(r)
	if username == "" {
		http.Redirect(w, r, "/ui/login", http.StatusFound)
		return
	}

	code := r.FormValue("code")

	admin, err := s.store.GetAdmin(username)
	if err != nil || admin == nil || !admin.MFAEnabled || admin.MFASecret == "" {
		s.logger.Error().Err(err).Msg("MFA not configured for admin during verify")
		http.Redirect(w, r, "/ui/login", http.StatusFound)
		return
	}

	if !validateTOTP(admin.MFASecret, code) {
		data := struct {
			Error string
		}{
			Error: "Invalid code. Please try again.",
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if err := templates.RenderMFAVerify(w, data); err != nil {
			s.logger.Error().Err(err).Msg("failed to render MFA verify with error")
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// Clear token_verified and create full session
	cookie, _ := r.Cookie(tokenVerifiedCookieName)
	if cookie != nil {
		s.sessions.delete(cookie.Value)
	}
	clearTokenVerifiedCookie(w)

	sessionToken, err := s.sessions.create(username)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to create session after MFA verify")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sessionToken)
	http.Redirect(w, r, "/ui/", http.StatusFound)
}

// handleUIAdmins renders the admin management page.
func (s *Server) handleUIAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := s.store.ListAdmins()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list admins for UI")
		admins = []Admin{}
	}
	if admins == nil {
		admins = []Admin{}
	}

	// Get current username from session
	currentUser := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentUser = s.sessions.getUsername(cookie.Value)
	}

	// Load all admin API tokens
	tokens, err := s.store.ListAllAdminAPITokens()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list admin API tokens for UI")
		tokens = []AdminAPIToken{}
	}
	if tokens == nil {
		tokens = []AdminAPIToken{}
	}

	data := struct {
		pageData
		Admins      []Admin
		CurrentUser string
		Tokens      []AdminAPIToken
	}{
		pageData:    pageData{Title: "Admins", Page: "admins"},
		Admins:      admins,
		CurrentUser: currentUser,
		Tokens:      tokens,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Render(w, data.Page, data); err != nil {
		s.logger.Error().Err(err).Msg("failed to render admins")
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// handleUIAdminCreate creates a new admin account.
func (s *Server) handleUIAdminCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" || req.DisplayName == "" {
		http.Error(w, `{"error":"username, display_name, and password are required"}`, http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
		return
	}

	// Check if username already exists
	existing, err := s.store.GetAdmin(req.Username)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if existing != nil {
		http.Error(w, `{"error":"username already exists"}`, http.StatusConflict)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to hash password")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	admin := &Admin{
		ID:           req.Username,
		DisplayName:  req.DisplayName,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}
	if err := s.store.CreateAdmin(admin); err != nil {
		s.logger.Error().Err(err).Msg("failed to create admin")
		http.Error(w, `{"error":"failed to create admin"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "username": req.Username})
}

// handleUIAdminDelete deletes an admin account.
func (s *Server) handleUIAdminDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing admin id"}`, http.StatusBadRequest)
		return
	}

	// Prevent deleting the last admin
	count, err := s.store.AdminCount()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if count <= 1 {
		http.Error(w, `{"error":"cannot delete the last admin account"}`, http.StatusBadRequest)
		return
	}

	// Prevent deleting yourself
	currentUser := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentUser = s.sessions.getUsername(cookie.Value)
	}
	if id == currentUser {
		http.Error(w, `{"error":"cannot delete your own account"}`, http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteAdmin(id); err != nil {
		http.Error(w, `{"error":"failed to delete admin"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// handleUIAdminChangePassword changes an admin's password.
func (s *Server) handleUIAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing admin id"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to hash password")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if err := s.store.UpdateAdminPassword(id, string(hash)); err != nil {
		http.Error(w, `{"error":"failed to update password"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// (Policy and department UI API calls now route directly to their handlers
// under session auth — no bootstrap token proxy needed.)
