package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnlineShellUsesSameOriginAssetsAndNoInlineAuthority(t *testing.T) {
	for _, path := range []string{"/", "/app.js", "/app.css"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("asset %s %d", path, w.Code)
		}
		if path == "/" && (!strings.Contains(w.Body.String(), "RunCradle") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'")) {
			t.Fatal("missing online shell/CSP")
		}
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/../go.mod", nil))
	if w.Code != 404 {
		t.Fatal("unregistered asset exposed")
	}
}

func TestWorkbenchShellSurfaces(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	for _, id := range []string{"workbench-canvas", "inspector", "launchpad", "file-picker", "diff-dialog", "evidence-console", "search-dialog"} {
		if !strings.Contains(w.Body.String(), `id="`+id+`"`) {
			t.Errorf("missing %s", id)
		}
	}
}

func TestWorkbenchModulesHaveExplicitSameOriginRoutes(t *testing.T) {
	for _, name := range []string{"state", "api", "editor", "runs", "inspector", "view", "feedback"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest("GET", "/workbench/"+name+".js", nil))
		if response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Content-Type") != "application/javascript; charset=utf-8" {
			t.Errorf("module %s: status=%d MIME=%q", name, response.Code, response.Header().Get("Content-Type"))
		}
	}
	for _, path := range []string{"/workbench/unknown.js", "/workbench/../style.css", "/workbench/../../go.mod"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 404 {
			t.Errorf("unregistered asset exposed: %s", path)
		}
	}
}
