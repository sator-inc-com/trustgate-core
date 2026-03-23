package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed templates/*.html
var templateFS embed.FS

// Templates holds parsed HTML templates, one per page.
type Templates struct {
	pages     map[string]*template.Template
	login     *template.Template
	mfaSetup  *template.Template
	mfaVerify *template.Template
}

// NewTemplates parses embedded HTML templates.
// Each page template is parsed separately with the layout to avoid
// template name conflicts (all pages define "content").
func NewTemplates() (*Templates, error) {
	funcMap := template.FuncMap{
		"pct": func(a, b int) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b) * 100
		},
	}

	pages := []string{"dashboard", "agents", "departments", "policies", "reports", "admins"}
	t := &Templates{pages: make(map[string]*template.Template)}

	layoutBytes, err := templateFS.ReadFile("templates/layout.html")
	if err != nil {
		return nil, err
	}

	for _, page := range pages {
		pageBytes, err := templateFS.ReadFile("templates/" + page + ".html")
		if err != nil {
			return nil, err
		}
		tmpl, err := template.New("").Funcs(funcMap).Parse(string(layoutBytes))
		if err != nil {
			return nil, err
		}
		if _, err := tmpl.Parse(string(pageBytes)); err != nil {
			return nil, err
		}
		t.pages[page] = tmpl
	}

	// Parse login template (standalone, no layout)
	loginBytes, err := templateFS.ReadFile("templates/login.html")
	if err != nil {
		return nil, err
	}
	loginTmpl, err := template.New("").Parse(string(loginBytes))
	if err != nil {
		return nil, err
	}
	t.login = loginTmpl

	// Parse MFA setup template (standalone, no layout)
	mfaSetupBytes, err := templateFS.ReadFile("templates/mfa_setup.html")
	if err != nil {
		return nil, err
	}
	mfaSetupTmpl, err := template.New("").Parse(string(mfaSetupBytes))
	if err != nil {
		return nil, err
	}
	t.mfaSetup = mfaSetupTmpl

	// Parse MFA verify template (standalone, no layout)
	mfaVerifyBytes, err := templateFS.ReadFile("templates/mfa_verify.html")
	if err != nil {
		return nil, err
	}
	mfaVerifyTmpl, err := template.New("").Parse(string(mfaVerifyBytes))
	if err != nil {
		return nil, err
	}
	t.mfaVerify = mfaVerifyTmpl

	return t, nil
}

// Render executes a page template by name.
// The name should be the page name (e.g., "dashboard", "agents").
func (t *Templates) Render(w io.Writer, page string, data any) error {
	tmpl, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("template not found: %s", page)
	}
	return tmpl.ExecuteTemplate(w, "layout", data)
}

// RenderLogin executes the login template.
func (t *Templates) RenderLogin(w io.Writer, data any) error {
	return t.login.ExecuteTemplate(w, "login", data)
}

// RenderMFASetup executes the MFA setup template.
func (t *Templates) RenderMFASetup(w io.Writer, data any) error {
	return t.mfaSetup.ExecuteTemplate(w, "mfa_setup", data)
}

// RenderMFAVerify executes the MFA verify template.
func (t *Templates) RenderMFAVerify(w io.Writer, data any) error {
	return t.mfaVerify.ExecuteTemplate(w, "mfa_verify", data)
}
