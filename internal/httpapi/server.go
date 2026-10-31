package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runcradle/internal/output"
	"runcradle/internal/protocol"
	"runcradle/internal/runs"
	"runcradle/internal/service"
	"strings"
	"sync"
	"time"
)

var errJSONMedia = errors.New("Content-Type application/json required")

type Config struct {
	Workspace, Binary, Helper string
	Port                      int
	Page                      http.Handler
}
type Server struct {
	URL          string
	Host         *service.Host
	Store        *runs.Store
	workspace    string
	descriptor   runs.ServerDescriptor
	http         *http.Server
	release      func() error
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	pairToken    string
	pairExpires  time.Time
	sessionToken string
	subscribers  map[string]int
	streams      chan struct{}
	once         sync.Once
	closeErr     error
	done         chan error
}
type Ready struct {
	Status     string `json:"status"`
	URL        string `json:"url"`
	InstanceID string `json:"instance_id"`
	PairingURL string `json:"pairing_url"`
}

func random(n int) (string, error) {
	b := make([]byte, n)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func Open(cfg Config) (*Server, error) {
	if os.Geteuid() == 0 {
		return nil, fmt.Errorf("serve requires non-root Linux amd64")
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("port must be 0..65535")
	}
	workspace, e := filepath.Abs(cfg.Workspace)
	if e != nil {
		return nil, e
	}
	workspace, e = filepath.EvalSymlinks(workspace)
	if e != nil {
		return nil, e
	}
	store, e := runs.Open(workspace, true)
	if e != nil {
		return nil, e
	}
	release, e := store.TryClaimServer()
	if e != nil {
		store.Close()
		return nil, fmt.Errorf("workspace host already active or unavailable: %w", e)
	}
	listener, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if e != nil {
		release()
		store.Close()
		return nil, e
	}
	token, e := random(32)
	if e != nil {
		listener.Close()
		release()
		store.Close()
		return nil, e
	}
	instance, e := random(16)
	if e != nil {
		listener.Close()
		release()
		store.Close()
		return nil, e
	}
	pair, e := random(32)
	if e != nil {
		listener.Close()
		release()
		store.Close()
		return nil, e
	}
	session, e := random(32)
	if e != nil {
		listener.Close()
		release()
		store.Close()
		return nil, e
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &Server{URL: "http://" + listener.Addr().String(), Store: store, workspace: workspace, release: release, ctx: ctx, cancel: stop, pairToken: pair, pairExpires: time.Now().Add(5 * time.Minute), sessionToken: session, streams: make(chan struct{}, 32), subscribers: map[string]int{}, done: make(chan error, 1)}
	s.Host = service.NewHost(store, cfg.Binary, cfg.Helper)
	s.descriptor = runs.ServerDescriptor{Version: 1, URL: s.URL, InstanceID: instance, Workspace: workspace, Token: token}
	mux := http.NewServeMux()
	s.routes(mux, cfg)
	s.http = &http.Server{Handler: s.boundary(mux), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if e = store.PublishServer(s.descriptor); e != nil {
		stop()
		listener.Close()
		release()
		store.Close()
		return nil, e
	}
	go func() {
		e := s.http.Serve(listener)
		if errors.Is(e, http.ErrServerClosed) {
			e = nil
		}
		s.done <- e
	}()
	return s, nil
}
func (s *Server) Ready() Ready {
	return Ready{"serving", s.URL, s.descriptor.InstanceID, s.URL + "/#pair=" + s.pairToken}
}
func (s *Server) Done() <-chan error { return s.done }
func (s *Server) Close() error {
	s.once.Do(func() {
		s.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		hostErr := s.Host.Close(ctx)
		httpErr := s.http.Shutdown(ctx)
		if httpErr != nil {
			httpErr = errors.Join(httpErr, s.http.Close())
		}
		s.closeErr = errors.Join(hostErr, httpErr, s.Store.ClearServer(s.descriptor.InstanceID), s.release())
		if hostErr == nil {
			s.closeErr = errors.Join(s.closeErr, s.Store.Close())
		}
	})
	return s.closeErr
}
func equal(a, b string) bool         { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *Server) cookieName() string { return "runcradle_" + s.descriptor.InstanceID[:12] }
func (s *Server) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-RunCradle-Instance", s.descriptor.InstanceID)
		w.Header().Set("X-RunCradle-Contract", protocol.ContractVersion)
		if r.Host != strings.TrimPrefix(s.URL, "http://") {
			fail(w, 403, fmt.Errorf("Host does not match this workspace host"))
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != s.URL {
			fail(w, 403, fmt.Errorf("cross-origin requests are forbidden"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/session" {
			bearer := equal(r.Header.Get("Authorization"), "Bearer "+s.descriptor.Token)
			cookie, e := r.Cookie(s.cookieName())
			browser := e == nil && equal(cookie.Value, s.sessionToken)
			if !bearer && !browser {
				fail(w, 401, fmt.Errorf("authenticate through the local pairing link"))
				return
			}
			if !bearer && r.Method != "GET" && r.Method != "HEAD" && origin != s.URL {
				fail(w, 403, fmt.Errorf("browser writes require same Origin"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || ct != "application/json" {
		return errJSONMedia
	}
	r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("exactly one JSON object required")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e := dec.Decode(v); e != nil {
		return e
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return fmt.Errorf("exactly one JSON object required")
	}
	return nil
}
func reply(w http.ResponseWriter, status int, v any) {
	raw, e := json.Marshal(v)
	if e != nil {
		fail(w, 500, e)
		return
	}
	if len(raw) > protocol.MaxResponseBytes {
		fail(w, 413, fmt.Errorf("response exceeds budget"))
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
func fail(w http.ResponseWriter, status int, e error) {
	reply(w, status, protocol.ProblemFor(status, output.Clean(e.Error(), 512)))
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.URL {
		fail(w, 403, fmt.Errorf("pairing requires same Origin"))
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if e := readJSON(w, r, &input); e != nil {
		apiError(w, e)
		return
	}
	s.mu.Lock()
	valid := s.pairToken != "" && time.Now().Before(s.pairExpires) && equal(input.Token, s.pairToken)
	if valid {
		s.pairToken = ""
	}
	s.mu.Unlock()
	if !valid {
		fail(w, 401, fmt.Errorf("pairing link expired or already used; restart serve to issue a new link"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: s.sessionToken, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	reply(w, 200, map[string]string{"status": "paired"})
}
