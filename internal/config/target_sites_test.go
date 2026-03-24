package config

import "testing"

func TestTargetSitesConfig_IsSiteAllowed(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TargetSitesConfig
		url     string
		want    bool
	}{
		{
			name: "empty include allows all",
			cfg:  TargetSitesConfig{},
			url:  "https://anything.com/path",
			want: true,
		},
		{
			name: "included site matches",
			cfg: TargetSitesConfig{
				Include: []string{"https://chatgpt.com/*"},
			},
			url:  "https://chatgpt.com/c/123",
			want: true,
		},
		{
			name: "non-included site rejected",
			cfg: TargetSitesConfig{
				Include: []string{"https://chatgpt.com/*"},
			},
			url:  "https://other-site.com/page",
			want: false,
		},
		{
			name: "excluded site rejected",
			cfg: TargetSitesConfig{
				Include: []string{"https://chatgpt.com/*", "https://docs.google.com/*"},
				Exclude: []string{"https://docs.google.com/*"},
			},
			url:  "https://docs.google.com/document/d/123",
			want: false,
		},
		{
			name: "included but not excluded passes",
			cfg: TargetSitesConfig{
				Include: []string{"https://chatgpt.com/*", "https://gemini.google.com/*"},
				Exclude: []string{"https://docs.google.com/*"},
			},
			url:  "https://gemini.google.com/app",
			want: true,
		},
		{
			name: "multiple includes, first matches",
			cfg: TargetSitesConfig{
				Include: []string{
					"https://chatgpt.com/*",
					"https://claude.ai/*",
				},
			},
			url:  "https://claude.ai/chat/abc",
			want: true,
		},
		{
			name: "exact match without wildcard",
			cfg: TargetSitesConfig{
				Include: []string{"chatgpt.com"},
			},
			url:  "chatgpt.com",
			want: true,
		},
		{
			name: "exact match fails on different string",
			cfg: TargetSitesConfig{
				Include: []string{"chatgpt.com"},
			},
			url:  "https://chatgpt.com/c/123",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsSiteAllowed(tt.url)
			if got != tt.want {
				t.Errorf("IsSiteAllowed(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
