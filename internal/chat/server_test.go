package chat

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

type fixedSnapshot Snapshot

func (f fixedSnapshot) Snapshot(int) Snapshot { return Snapshot(f) }

func get(t *testing.T, url, host string) (*http.Response, string) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		request.Host = host
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	return response, string(body)
}

func newServer(t *testing.T, snapshot Snapshot) *Server {
	t.Helper()

	server, err := Serve(fixedSnapshot(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	return server
}

func TestServerServesThePageUnderItsSecretWithAStrictPolicy(t *testing.T) {
	server := newServer(t, Snapshot{})

	response, body := get(t, server.URL, "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "CHAT REPLAY") {
		t.Fatalf("status %d, body %.80q", response.StatusCode, body)
	}

	policy := response.Header.Get("Content-Security-Policy")
	nonce := regexp.MustCompile(`script-src 'nonce-([0-9a-f]{32})'`).FindStringSubmatch(policy)
	if nonce == nil {
		t.Fatalf("the policy must allow only the script with a nonce: %q", policy)
	}
	if got := strings.Count(body, `nonce="`+nonce[1]+`"`); got != 2 {
		t.Errorf("the script and the style carry the nonce, found %d", got)
	}
	if strings.Contains(body, "{{nonce}}") {
		t.Error("an unreplaced placeholder")
	}
	for _, want := range []string{"default-src 'none'", "connect-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("the policy lacks %q: %q", want, policy)
		}
	}
	if response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Cache-Control") != "no-store" ||
		response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers: %v", response.Header)
	}

	again, _ := get(t, server.URL, "")
	if other := regexp.MustCompile(`'nonce-([0-9a-f]{32})'`).FindStringSubmatch(again.Header.Get("Content-Security-Policy")); other[1] == nonce[1] {
		t.Error("every response needs a new nonce")
	}
}

func TestServerGivesTheMessagesAsDataNotAsMarkup(t *testing.T) {
	server := newServer(t, Snapshot{
		Title: "<b>t</b>", Time: 12.5,
		Messages: []Message{{Time: 1, Author: "@a", Text: `<script>alert(1)</script> & "q"`, Kind: 0}},
	})

	response, body := get(t, server.URL+"messages", "")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "<b>") {
		t.Errorf("markup characters must be escaped in the JSON: %s", body)
	}

	var got Snapshot
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Messages[0].Text != `<script>alert(1)</script> & "q"` || got.Time != 12.5 {
		t.Errorf("the data must survive the escaping: %+v, %v", got, err)
	}
}

func TestServerRefusesWhatIsNotItsPageAndSecret(t *testing.T) {
	server := newServer(t, Snapshot{})
	base := strings.TrimSuffix(server.URL, "/"+server.secret+"/")

	for name, url := range map[string]string{
		"no secret":      base + "/",
		"wrong secret":   base + "/" + strings.Repeat("0", 32) + "/",
		"short secret":   base + "/" + server.secret[:31] + "/",
		"messages, no s": base + "/messages",
		"unknown path":   server.URL + "admin",
		"traversal":      server.URL + "../messages",
	} {
		if response, _ := get(t, url, ""); response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusMovedPermanently {
			t.Errorf("%s: status %d, want 404", name, response.StatusCode)
		}
	}
}

func TestServerRefusesAnotherHostName(t *testing.T) {
	server := newServer(t, Snapshot{})

	// A rebound name points at 127.0.0.1 but is sent as the host.
	for _, host := range []string{"evil.example", "evil.example:" + portOf(server), "localhost:" + portOf(server), "127.0.0.1"} {
		if response, _ := get(t, server.URL+"messages", host); response.StatusCode != http.StatusNotFound {
			t.Errorf("host %q: status %d, want 404", host, response.StatusCode)
		}
	}
}

func TestServerOnlyAnswersReads(t *testing.T) {
	server := newServer(t, Snapshot{})

	response, err := http.Post(server.URL+"messages", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d", response.StatusCode)
	}
}

func TestServerListensOnTheLoopbackOnly(t *testing.T) {
	server := newServer(t, Snapshot{})

	addr := server.listener.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() || addr.Port == 0 {
		t.Errorf("listening on %v", addr)
	}
	if !strings.HasPrefix(server.URL, "http://127.0.0.1:") {
		t.Errorf("URL = %q", server.URL)
	}
}

func TestServerStopsOnClose(t *testing.T) {
	server := newServer(t, Snapshot{})
	server.Close()

	if _, err := http.Get(server.URL); err == nil {
		t.Error("a closed server must not answer")
	}
}

func portOf(s *Server) string {
	_, port, _ := net.SplitHostPort(s.host)
	return port
}
