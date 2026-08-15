# Contributing to Grix

Thank you for contributing to Grix.

## Development

Keep changes within the existing backend service boundaries and Flutter
feature modules unless an architectural change is part of the proposal.

Before submitting a pull request:

1. Add or update focused tests for behavior changes.
2. Run the narrowest relevant tests, followed by broader package checks when
   the change crosses module boundaries.
3. Describe API or WebSocket contract changes in the pull request.
4. Confirm the diff contains no credentials, private infrastructure details,
   generated build output, or local machine paths.
5. Attribute commits only to the people who contributed. Do not add
   `Co-authored-by` trailers for AI tools or coding agents, including Claude
   Code and Cursor.

Run the repository's secret scan before submitting security-sensitive changes.
The committed `.gitleaks.toml` contains only narrow exceptions for public
protocol constants and generated checksums.

Use an issue or draft pull request to discuss substantial protocol or storage
changes before investing in a large implementation.
