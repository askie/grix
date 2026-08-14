# Contributing to Grix

Thank you for contributing to Grix.

## Development

Start with [the development guide](docs/develop.md). Keep changes within the
existing backend service boundaries and Flutter feature modules unless an
architectural change is part of the proposal.

Before submitting a pull request:

1. Add or update focused tests for behavior changes.
2. Run the narrowest relevant tests, followed by broader package checks when
   the change crosses module boundaries.
3. Update API or WebSocket documentation when a public contract changes.
4. Confirm the diff contains no credentials, private infrastructure details,
   generated build output, or local machine paths.

Run the repository's secret scan before submitting security-sensitive changes.
The committed `.gitleaks.toml` contains only narrow exceptions for public
protocol constants and generated checksums.

Use an issue or draft pull request to discuss substantial protocol or storage
changes before investing in a large implementation.
