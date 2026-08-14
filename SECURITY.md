# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities through GitHub's private vulnerability
reporting feature for this repository. Do not open a public issue containing
exploit details, credentials, private deployment information, or user data.

Include the affected version or commit, reproduction steps, expected impact,
and any suggested mitigation. The maintainers will acknowledge a complete
report, assess severity, and coordinate disclosure and remediation.

## Secrets

Never commit production credentials, private keys, environment files,
kubeconfig files, Terraform state, or plaintext Kubernetes Secrets. Examples
must use unmistakably synthetic values.
