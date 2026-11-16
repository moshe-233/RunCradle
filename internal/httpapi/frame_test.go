package httpapi

import (
	"net/http"
	"runcradle/internal/webui"
	"strings"
	"testing"
)

func TestWorkbenchFramePolicyIsOptInAndDoesNotRelaxAPIAuthentication(t *testing.T) {
	for _, origin := range []string{"", "http://127.0.0.1:3085"} {
		t.Run(origin, func(t *testing.T) {
			s, err := Open(Config{Workspace: t.TempDir(), Binary: "/bin/true", Helper: "/bin/true", Page: webui.Handler(), FrameOrigin: origin})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			res, err := http.Get(s.URL + "/")
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			policy := strings.Join(res.Header.Values("Content-Security-Policy"), "; ")
			ancestor := "'none'"
			if origin != "" {
				ancestor = origin
			}
			if !strings.Contains(policy, "frame-ancestors "+ancestor) || !strings.Contains(policy, "connect-src 'self'") {
				t.Fatal(policy)
			}
			if (res.Header.Get("X-Frame-Options") == "DENY") != (origin == "") {
				t.Fatal("unexpected legacy frame policy")
			}
			if code, _ := callAPI(t, s, "GET", "/api/v1/workflows", nil, false); code != 401 {
				t.Fatalf("API auth relaxed: %d", code)
			}
		})
	}
	for _, origin := range []string{"https://example.com", "http://localhost:3085", "http://127.0.0.1:3085/", "http://127.0.0.1:3085/path", "http://127.0.0.1:3085?x", "http://127.0.0.1:99999", "http://user@127.0.0.1:3085"} {
		if s, err := Open(Config{FrameOrigin: origin}); err == nil {
			s.Close()
			t.Errorf("accepted invalid origin: %s", origin)
		}
	}
}
