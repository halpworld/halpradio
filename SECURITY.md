# Security Policy

## Supported Versions

We actively provide security patches and updates for the following versions of **halpradio**:

| Version | Supported          |
| ------- | ------------------ |
| 0.0.x   | :white_check_mark: |
| < 0.0.1 | :x:                |

## Reporting a Vulnerability

The **halpradio** team takes the security of our application and users seriously.

If you believe you have discovered a security vulnerability in `halpradio`:

1. **Do not create a public issue** on GitHub.
2. Please disclose the vulnerability responsibly via [GitHub Private Vulnerability Reporting](https://github.com/halpworld/halpradio/security/advisories/new) or by emailing the project maintainers directly.
3. Include detailed steps to reproduce the vulnerability, along with proof-of-concept payload or code if possible.
4. We will acknowledge receipt of your report within 48 hours and work with you to release a fix promptly.

## Security Best Practices for Users

- **Streaming URLs**: `halpradio` plays user-provided and community-submitted audio URLs. We recommend streaming over HTTPS whenever possible.
- **Custom Config**: Custom stations are stored locally in `~/.config/halpradio/stations.yaml`. Keep this directory with standard user read/write permissions.
