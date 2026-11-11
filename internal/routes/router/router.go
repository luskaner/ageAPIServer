// Package router is the HTTP plumbing: a thin group wrapper over
// http.ServeMux plus the middleware chain. The route packages register handlers
// here and never touch the mux directly.
package router

import (
	"context"
	"net/http"

	"github.com/luskaner/ageAPIServer/internal/models"
)

// Group is a path prefix over a shared mux.
type Group struct {
	parent *Group
	path   string
	mux    *http.ServeMux
}

// New creates the root group.
func New() *Group {
	return &Group{path: "", mux: http.NewServeMux()}
}

// Subgroup creates a child group. The path is appended to the parent's.
func (g *Group) Subgroup(path string) *Group {
	return &Group{parent: g, path: path, mux: g.mux}
}

func (g *Group) fullPath() string {
	if g.parent == nil {
		return g.path
	}
	return g.parent.fullPath() + g.path
}

// HandleFunc registers a handler for one method and path.
func (g *Group) HandleFunc(method string, path string, handler http.HandlerFunc) {
	g.mux.HandleFunc(method+" "+g.fullPath()+path, handler)
}

// Handle registers a handler for one method and path.
func (g *Group) Handle(method string, path string, handler http.Handler) {
	g.mux.Handle(method+" "+g.fullPath()+path, handler)
}

// HandlePath registers a handler for every method.
func (g *Group) HandlePath(path string, handler http.Handler) {
	g.mux.Handle(g.fullPath()+path, handler)
}

// Mux exposes the underlying mux so the caller can wrap it once.
func (g *Group) Mux() *http.ServeMux {
	return g.mux
}

// Middleware is the standard net/http middleware shape.
type Middleware func(http.Handler) http.Handler

// Chain applies the middlewares so the first one is the outermost.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// viewerKey is the context key of the resolved identity.
type viewerKey struct{}

// WithViewer stores the identity in the request context.
func WithViewer(ctx context.Context, viewer models.Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, viewer)
}

// ViewerFrom returns the identity of the request, anonymous when absent.
func ViewerFrom(r *http.Request) models.Viewer {
	viewer, ok := r.Context().Value(viewerKey{}).(models.Viewer)
	if !ok {
		return models.Viewer{}
	}
	return viewer
}
