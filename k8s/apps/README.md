# Grix application manifests

`k8s/apps/base` contains the public Kustomize baseline for the API, WebSocket,
LLM, push, and migration workloads.

Copy the example Secret manifests to an ignored local path and replace every
placeholder before applying the configuration. Never commit rendered Secrets.

Production overlays, registry coordinates, release automation, and environment
specific values are intentionally maintained outside the public repository.

Before deploying, review resource requests, storage classes, ingress settings,
image repositories, and security policies for your own cluster.
