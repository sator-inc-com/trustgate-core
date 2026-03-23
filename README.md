# TrustGate

**AI Zero Trust Gateway** — Inspect and control AI input/output in real-time.

TrustGate deploys as a sidecar proxy or desktop agent, intercepting AI traffic to detect prompt injection, PII leakage, and confidential data exposure — all processed locally with zero external API calls.

## Features

- **Two-stage detection**: Regex (<5ms) + Prompt Guard 2 86M (1-5ms, gray-zone only)
- **OpenAI-compatible API**: Drop-in proxy for Amazon Bedrock
- **Inspection API**: `/v1/inspect` for browser extension / third-party integration
- **Policy engine**: YAML-defined rules with shadow mode, whitelists, severity levels
- **Audit log**: JSONLines WAL with SHA256 hash chain (tamper detection)
- **Zero trust**: Raw text never leaves the customer environment — only hashes and metadata

## Quick Start

```bash
# Build
go build -o aigw ./cmd/aigw

# Initialize
./aigw init --provider bedrock --with-samples

# Check environment
./aigw doctor

# Run (no AWS credentials needed)
./aigw serve --mock-backend

# Test
curl -s http://localhost:8787/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"test","messages":[{"role":"user","content":"hello"}]}' | jq .
```

## Architecture

```
aigw-server (Control Plane :9090)  ← policy, config, reports
    ↑ stats push (60s) / policy pull
┌──────────┐ ┌──────────┐ ┌──────────┐
│App+Agent │ │App+Agent │ │App+Agent │  ← sidecar or Desktop Agent
│  :8787   │ │  :8787   │ │  :8787   │
└────┬─────┘ └────┬─────┘ └────┬─────┘
     ▼             ▼            ▼
  Bedrock       Bedrock      Bedrock
```

## Product Lines

| | for Applications | for Workforce |
|---|---|---|
| Use case | Protect self-built AI systems | Monitor employee AI SaaS usage |
| Deployment | Sidecar proxy | Desktop Agent + browser extension |
| API | `/v1/chat/completions` | `/v1/inspect` |

## Deployment Tiers

- **Free**: Agent only (standalone, local policy, in-memory audit)
- **Pro**: Agent + TrustGate Cloud (SaaS control plane)
- **Enterprise**: Agent + on-prem Control Plane

## License

[BSL 1.1](LICENSE) — Converts to Apache 2.0 after 4 years.
