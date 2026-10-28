package webui

import (
	"net/http"
)

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, mime := "", ""
		switch r.URL.Path {
		case "/":
			name, mime = "shell.html", "text/html; charset=utf-8"
		case "/app.js":
			name, mime = "shell.js", "application/javascript; charset=utf-8"
		case "/graph.js":
			name, mime = "graph.js", "application/javascript; charset=utf-8"
		case "/theme.js":
			name, mime = "theme.js", "application/javascript; charset=utf-8"
		case "/app.css":
			name, mime = "shell.css", "text/css; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		raw, e := assets.ReadFile("assets/" + name)
		if e != nil {
			http.Error(w, "UI asset unavailable", 500)
			return
		}
		if name == "shell.css" {
			theme, _ := assets.ReadFile("assets/theme.css")
			raw = append(theme, raw...)
			extra, _ := assets.ReadFile("assets/graph.css")
			raw = append(raw, extra...)
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		_, _ = w.Write(raw)
	})
}
