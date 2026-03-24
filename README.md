# TrustGate for Workforce

**AI Zero Trust Gateway** — Monitor and control employee AI usage in real time.

TrustGate inspects text sent to AI services (ChatGPT, Gemini, Claude.ai, Copilot) to detect prompt injection, PII leakage, and confidential data exposure — all processed locally with zero external API calls.

## Features

- **Real-time inspection**: Detects PII, prompt injection, and confidential data before submission
- **Browser extension**: Intercepts AI input/output at the DOM level (no HTTPS interception)
- **Two-stage detection**: Regex (<5ms) + Prompt Guard 2 86M (1-5ms, gray-zone only)
- **Policy engine**: YAML-defined rules with shadow mode, whitelists, severity levels
- **Site lockout**: 30-second block with full DOM replacement on policy violation
- **Audit log**: SHA256 hash chain — raw text never leaves the customer environment
- **Desktop Agent**: Runs locally as a system service (Windows Service / launchd / systemd)

## Supported AI Services

| Service | URL |
|---------|-----|
| ChatGPT | chatgpt.com |
| Gemini | gemini.google.com |
| Claude.ai | claude.ai |
| Copilot | copilot.microsoft.com |

## Installation

Download the installer for your platform from the [Releases](https://github.com/sator-inc-com/trustgate-core/releases/latest) page.

### Windows

1. Download and run `TrustGate-Windows-*.exe`
2. Follow the installer wizard (choose Standalone or Managed mode)
3. TrustGate Agent starts automatically as a Windows Service

### macOS

1. Download `TrustGate-macOS-*-arm64.pkg`
2. Double-click to run the installer
3. TrustGate Agent starts automatically as a launchd service

### Linux (Debian/Ubuntu)

```bash
sudo dpkg -i TrustGate-Linux-*-amd64.deb
# Agent starts automatically via systemd
sudo systemctl status trustgate
```

### Verify Installation

```bash
curl -s http://localhost:8787/v1/health | jq .
```

## How It Works

```
Employee PC
┌─────────────────────────────────────────┐
│  Browser + TrustGate Extension          │
│    ↓ text capture (DOM/fetch intercept) │
│  TrustGate Agent (localhost:8787)       │
│    ↓ inspect → detect → policy check   │
│    ↓ ALLOW / WARN / BLOCK              │
│  AI Service (ChatGPT, Gemini, etc.)    │
└─────────────────────────────────────────┘
```

The browser extension captures text from AI service input fields and sends it to the local Agent via `POST /v1/inspect`. The Agent runs detection locally, evaluates policies, and returns an action. On BLOCK, the extension replaces the page with a 30-second lockout screen.

## Deployment Tiers

- **Free**: Agent only (standalone, local policy, in-memory audit)
- **Pro**: Agent + TrustGate Cloud (SaaS control plane, managed policies, reports)
- **Enterprise**: Agent + on-prem Control Plane (self-hosted)

## Configuration

After installation, the Agent config is located at:

| Platform | Path |
|----------|------|
| Windows | `C:\ProgramData\TrustGate\agent.yaml` |
| macOS | `/Library/Application Support/TrustGate/agent.yaml` |
| Linux | `/etc/trustgate/agent.yaml` |

## License

[BSL 1.1](LICENSE) — Converts to Apache 2.0 after 4 years.
