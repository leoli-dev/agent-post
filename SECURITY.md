# Security Policy

## Current status

This repository contains an initial source implementation. No versioned binary release or long-term support policy is published. API key and exact-source-IP checks, administrator sessions/CSRF, current-token claims, and retention controls are implemented and exercised by local tests; this is not a claim of a complete security audit. See [runtime](docs/runtime.md) and [validation](docs/validation.md) for operational boundaries and actual evidence.

## Private reporting

After the repository is published on GitHub and private vulnerability reporting is enabled, use GitHub's private vulnerability reporting and security advisory facilities for sensitive findings. This document does not assert that a private reporting channel is already enabled, and it provides no alternative contact address.

Do not put credentials, confidential messages, private host information, or actionable vulnerability details in public issues or pull requests. A private report should describe the affected document or version, the security boundary involved, the expected and observed behavior, and a minimal reproduction with secrets removed.

Maintainers will use the private advisory process to assess findings and coordinate any correction and disclosure. This policy does not establish a response-time commitment or a claim that a security audit has been completed.

Ordinary documentation errors without sensitive security details can be reported through the normal issue process described in [CONTRIBUTING.md](CONTRIBUTING.md).
