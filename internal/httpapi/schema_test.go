package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestOpenAPICoversEveryProductRouteAndResolvesReferences(t *testing.T) {
	spec := OpenAPI()
	paths := spec["paths"].(schema)
	registered := map[string]bool{}
	for _, file := range []string{"workflows.go", "runs.go", "protocol.go"} {
		node, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "HandleFunc" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.SplitN(pattern, " ", 2)
			if len(parts) != 2 || !strings.HasPrefix(parts[1], "/api/v1/") {
				return true
			}
			registered[strings.ToLower(parts[0])+" "+parts[1]] = true
			return true
		})
	}
	for path, value := range paths {
		for method := range value.(schema) {
			key := method + " " + path
			if !registered[key] {
				t.Errorf("schema advertises unregistered route: %s", key)
			}
			delete(registered, key)
		}
	}
	for key := range registered {
		t.Errorf("route missing from contract: %s", key)
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case schema:
			if reference, ok := v["$ref"].(string); ok {
				name := strings.TrimPrefix(reference, "#/components/schemas/")
				if spec["components"].(schema)["schemas"].(schema)[name] == nil {
					t.Errorf("unresolved reference: %s", reference)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case []schema:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(spec)
}

func TestUnknownAPIDoesNotFallThroughToWebUI(t *testing.T) {
	s, err := Open(Config{Workspace: t.TempDir(), Binary: "/bin/true", Helper: "/bin/true", Page: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("UI")) })})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	code, raw := callAPI(t, s, "GET", "/api/v1/unknown", nil, true)
	if code != 404 || !strings.Contains(string(raw), `"code":"not_found"`) {
		t.Fatalf("API fell through to UI: %d %s", code, raw)
	}
	mux := http.NewServeMux()
	s.routes(mux, Config{})
	res := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/workflows", nil)
	mux.ServeHTTP(res, req)
	if res.Code != 405 || res.Header().Get("Allow") != "GET, HEAD, POST" {
		t.Fatalf("method contract: %d %s", res.Code, res.Header().Get("Allow"))
	}
}
