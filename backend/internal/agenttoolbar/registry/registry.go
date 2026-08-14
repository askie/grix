package registry

import "github.com/askie/grix/backend/internal/agenttoolbar/core"

type Registry struct {
	packages []core.Package
}

func New() *Registry {
	return &Registry{}
}

func (r *Registry) Register(pkg core.Package) {
	if r == nil || pkg == nil {
		return
	}
	r.packages = append(r.packages, pkg)
}

func (r *Registry) Resolve(ctx core.MatchContext) core.Package {
	if r == nil {
		return nil
	}
	for _, pkg := range r.packages {
		if pkg != nil && pkg.Match(ctx) {
			return pkg
		}
	}
	return nil
}
