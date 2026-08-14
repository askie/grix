# Admin web placeholder

This directory is intentionally committed so `go:embed all:dist` has a stable
source directory in public checkouts.

The generated admin web bundle is not stored in this repository. A real build
must provide `index.html` and assets here before serving `/admin`.
