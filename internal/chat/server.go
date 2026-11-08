package chat

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/geovannewashington/ytwatch/internal/youtube"
)

// maxShown is how many messages the page gets at a time.
const maxShown = 200

// Snapshotter is what the server shows. A *Session is one.
type Snapshotter interface {
	Snapshot(limit int) Snapshot
}

// Server serves the chat page to the browser window that ytwatch opens.
//
// It is a web server on the user's machine, so it is kept as small as it can
// be: it listens on the loopback address only, the page and the data are under
// a random secret in the URL, a request for another host name is refused (a
// page from the internet that points its name at 127.0.0.1 must not be able to
// read it), and the page may run nothing but its own script.
type Server struct {
	// URL is the address of the page, with its secret.
	URL string

	listener net.Listener
	server   *http.Server
	secret   string
	host     string
	source   Snapshotter
}

// Serve starts a Server for source. Stop it with Close.
func Serve(source Snapshotter) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start the chat page: %w", err)
	}

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		listener.Close()
		return nil, fmt.Errorf("start the chat page: %w", err)
	}

	s := &Server{
		listener: listener,
		secret:   hex.EncodeToString(raw),
		host:     listener.Addr().String(),
		source:   source,
	}
	s.URL = "http://" + s.host + "/" + s.secret + "/"

	s.server = &http.Server{
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}

	go func() { _ = s.server.Serve(listener) }()

	return s, nil
}

// Close stops the server.
func (s *Server) Close() { _ = s.server.Close() }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// DNS rebinding: a page on evil.example whose name points at 127.0.0.1
	// sends its own name as the host, not ours.
	if r.Host != s.host {
		http.NotFound(w, r)
		return
	}

	secret, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.secret)) != 1 {
		http.NotFound(w, r)
		return
	}

	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")

	switch rest {
	case "":
		s.page(w)
	case "messages":
		s.messages(w)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) page(w http.ResponseWriter) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce := hex.EncodeToString(raw)

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+
		"'; img-src "+strings.Join(youtube.ChatImageHosts, " ")+
		"; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")

	_, _ = w.Write([]byte(strings.ReplaceAll(pageHTML, "{{nonce}}", nonce)))
}

func (s *Server) messages(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(s.source.Snapshot(maxShown))
}
