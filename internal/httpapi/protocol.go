package httpapi

import (
	"net/http"
	"runcradle/internal/protocol"
	"strings"
)

func (s *Server) protocolRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/protocol", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, protocol.Describe()) })
	m.HandleFunc("GET /api/v1/openapi.json", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, OpenAPI()) })
	// The browser's root handler and ServeMux's plain-text 404/405 must never
	// swallow API failures. Probe registered methods to preserve an honest Allow.
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		var allowed []string
		for _, method := range []string{"GET", "HEAD", "POST", "PUT"} {
			probe := r.Clone(r.Context())
			probe.Method = method
			_, pattern := m.Handler(probe)
			if strings.Contains(pattern, " /api/v1/") {
				allowed = append(allowed, method)
			}
		}
		status := http.StatusNotFound
		if len(allowed) > 0 {
			status = http.StatusMethodNotAllowed
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		fail(w, status, protocolError(http.StatusText(status)))
	})
}

type protocolError string

func (e protocolError) Error() string { return string(e) }
