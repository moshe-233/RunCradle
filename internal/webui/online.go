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
		case "/workbench/feedback.js", "/workbench/state.js", "/workbench/api.js", "/workbench/editor.js", "/workbench/runs.js", "/workbench/inspector.js", "/workbench/view.js":
			// 只开放在线模块白名单；离线导出继续内嵌自己的独立脚本。
			name, mime = r.URL.Path[1:], "application/javascript; charset=utf-8"
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
		// The HTTP server supplies frame-ancestors (DENY by default, or one
		// explicitly configured local DSH origin). Keep both policies active.
		w.Header().Add("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src data:; object-src 'none'; base-uri 'none'; form-action 'none'")
		_, _ = w.Write(raw)
	})
}
