# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Sherlock, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please report vulnerabilities via one of these methods:

1. **GitHub Private Vulnerability Reporting**: Use the "Report a vulnerability" button on the Security tab of this repository
2. **Email**: Send details to the repository maintainers

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgement**: Within 48 hours of report
- **Initial Assessment**: Within 7 days
- **Fix Target**: Within 90 days for confirmed vulnerabilities
- **Disclosure**: Coordinated disclosure after fix is available

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | Yes                |

## Security Practices

Sherlock follows these security practices:

- All inbound webhooks are verified using source-specific signatures (HMAC-SHA256 for Grafana, Slack signing secret verification)
- Secrets are never logged or stored in plaintext debug output
- Kubernetes access uses least-privilege RBAC (read-only by default)
- Slack tokens are stored securely and never serialized in API responses
- Raw payloads are stored in object storage, not in database columns
- Optional LLM features (future) will redact sensitive data before sending

## Dependency Management

- Dependencies are tracked in go.mod with specific versions
- CI runs vulnerability scanning on dependencies
- Security updates are applied promptly
