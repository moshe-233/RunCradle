package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runcradle/internal/protocol"
	"strings"
	"testing"
)

func TestRequestBoundaryClassifiesMediaAndSize(t *testing.T) {
	s := apiFixture(t)
	for _, tc := range []struct {
		media, body string
		status      int
		code        string
	}{
		{"text/plain", `{}`, 415, "unsupported_media_type"},
		{"application/json", `{"source":"` + strings.Repeat("x", protocol.MaxRequestBytes) + `"}`, 413, "budget_exceeded"},
		{"application/json", `{"unknown":1}`, 400, "invalid_request"},
		{"application/json", `{`, 400, "invalid_request"},
	} {
		req, err := http.NewRequest("POST", s.URL+"/api/v1/validate", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+s.descriptor.Token)
		req.Header.Set("Content-Type", tc.media)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var problem protocol.Problem
		if err := json.Unmarshal(raw, &problem); err != nil || res.StatusCode != tc.status || problem.Code != tc.code {
			t.Fatalf("boundary: %d %s %v", res.StatusCode, raw, err)
		}
	}
}

func TestPairingUsesTheSameRequestMediaClassification(t *testing.T) {
	s := apiFixture(t)
	req, err := http.NewRequest("POST", s.URL+"/api/v1/session", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 415 {
		t.Fatalf("pairing media classification: %d", res.StatusCode)
	}
}

func TestProtocolDiscoveryIsAuthenticatedAndEngineIndependent(t *testing.T) {
	s := apiFixture(t)
	for _, path := range []string{"/api/v1/protocol", "/api/v1/openapi.json"} {
		if code, _ := callAPI(t, s, "GET", path, nil, false); code != 401 {
			t.Fatalf("%s auth: %d", path, code)
		}
		code, raw := callAPI(t, s, "GET", path, nil, true)
		if code != 200 || !json.Valid(raw) {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
		if strings.Contains(string(raw), s.descriptor.Token) || strings.Contains(string(raw), s.pairToken) {
			t.Fatal("discovery leaks credentials")
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if path == "/api/v1/protocol" {
			if string(doc["api_version"]) != `"v1"` || len(doc["limits"]) == 0 || len(doc["contract_version"]) == 0 {
				t.Fatalf("missing contract: %s", raw)
			}
		} else if string(doc["openapi"]) != `"3.1.1"` || len(doc["paths"]) == 0 || len(doc["components"]) == 0 {
			t.Fatalf("missing OpenAPI: %s", raw)
		}
	}
}

func TestAPIErrorsHaveStableCodesIncludingRoutingFailures(t *testing.T) {
	s := apiFixture(t)
	for _, tc := range []struct {
		method, path string
		body         any
		auth         bool
		status       int
		code         string
	}{
		{"GET", "/api/v1/health", nil, false, 401, "unauthorized"},
		{"GET", "/api/v1/not-a-route", nil, true, 404, "not_found"},
		{"DELETE", "/api/v1/workflows", nil, true, 405, "method_not_allowed"},
		{"POST", "/api/v1/validate", map[string]any{"unexpected": true}, true, 400, "invalid_request"},
	} {
		status, raw := callAPI(t, s, tc.method, tc.path, tc.body, tc.auth)
		var problem struct {
			Code    string `json:"code"`
			Message string `json:"error"`
			Status  int    `json:"status"`
			Next    string `json:"next_action"`
		}
		if err := json.Unmarshal(raw, &problem); err != nil || status != tc.status || problem.Status != status || problem.Code != tc.code || problem.Message == "" || problem.Next == "" {
			t.Fatalf("%s %s: %d %s (%v)", tc.method, tc.path, status, raw, err)
		}
	}
}

func TestReadJSONRequiresOneObject(t *testing.T) {
	for _, raw := range []string{"null", "[]", `{} {}`, `{"token":"a","extra":1}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		var input struct {
			Token string `json:"token"`
		}
		if err := readJSON(httptest.NewRecorder(), r, &input); err == nil {
			t.Fatalf("accepted non-object/invalid input: %s", raw)
		}
	}
}

func TestSaveRejectsUnappliedEditAndSchemaDoesNotAdvertiseIt(t *testing.T) {
	s := apiFixture(t)
	code, raw := callAPI(t, s, "POST", "/api/v1/workflows", map[string]any{"format": "yaml", "source": "processes: {a: {command: echo old}}"}, true)
	var created struct {
		Workflow struct {
			ID       string `json:"workflow_id"`
			Revision string `json:"revision"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || code != 201 {
		t.Fatalf("create: %d %s %v", code, raw, err)
	}
	for _, key := range []string{"edit", "workbench_edit"} {
		body := map[string]any{"format": "yaml", "source": "processes: {a: {command: echo old}}", "expected_revision": created.Workflow.Revision, key: map[string]any{}}
		code, raw = callAPI(t, s, "PUT", "/api/v1/workflows/"+created.Workflow.ID, body, true)
		if code != 400 {
			t.Fatalf("silently ignored %s: %d %s", key, code, raw)
		}
	}
	save := OpenAPI()["components"].(schema)["schemas"].(schema)["SaveRequest"].(schema)
	for _, key := range []string{"edit", "workbench_edit"} {
		if _, ok := save["properties"].(schema)[key]; ok {
			t.Fatalf("schema advertises ignored %s", key)
		}
	}
}
