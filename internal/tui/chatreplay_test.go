package tui

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/config"
	"github.com/geovannewashington/ytwatch/internal/media"
)

func vodWithChat() media.Video {
	return media.Video{ID: "abcdefghijk", Title: "A VOD", LiveStatus: "was_live", ChatReplayToken: "tok"}
}

func fakeChatWindow(t *testing.T) *[]string {
	t.Helper()

	old := openChatWindow
	t.Cleanup(func() { openChatWindow = old })

	var urls []string
	openChatWindow = func(url string, _ config.Config) error {
		urls = append(urls, url)
		return nil
	}

	return &urls
}

func TestOpenChatReplayOpensALocalPageInTheChatWindow(t *testing.T) {
	urls := fakeChatWindow(t)
	m := New("test")
	m.cfg.Chat.Enabled = true

	socket := filepath.Join(t.TempDir(), "gone.sock") // mpv is not there: the page says so
	if err := m.openChatReplay(vodWithChat(), socket); err != nil {
		t.Fatal(err)
	}
	if len(*urls) != 1 || !strings.HasPrefix((*urls)[0], "http://127.0.0.1:") || !m.chatOpened["abcdefghijk"] {
		t.Fatalf("urls = %v, opened = %v", *urls, m.chatOpened)
	}

	// The window gets a page, and mpv being gone is told to it.
	var body string
	for range 50 {
		response, err := http.Get((*urls)[0] + "messages")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if body = string(data); strings.Contains(body, "Playback ended") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(body, "Playback ended") || !strings.Contains(body, "A VOD") {
		t.Errorf("messages = %s", body)
	}

	// Opening again for the same playback must not open a second window.
	if err := m.openChatReplay(vodWithChat(), socket); err != nil || len(*urls) != 1 {
		t.Errorf("a second window opened: %v, %v", *urls, err)
	}

	m.cancel()
}

func TestOpenChatReplayDoesNothingWithoutWhatItNeeds(t *testing.T) {
	urls := fakeChatWindow(t)
	socket := filepath.Join(t.TempDir(), "mpv.sock")

	live := vodWithChat()
	live.LiveStatus = "is_live"
	noToken := vodWithChat()
	noToken.ChatReplayToken = ""

	for name, tc := range map[string]struct {
		video   media.Video
		socket  string
		enabled bool
	}{
		"chat turned off": {vodWithChat(), socket, false},
		"no socket":       {vodWithChat(), "", true},
		"no replay":       {noToken, socket, true},
		"live stream":     {live, socket, true},
	} {
		m := New("test")
		m.cfg.Chat.Enabled = tc.enabled
		if err := m.openChatReplay(tc.video, tc.socket); err != nil || m.chatOpened[tc.video.ID] {
			t.Errorf("%s: err = %v, opened = %v", name, err, m.chatOpened)
		}
	}
	if len(*urls) != 0 {
		t.Errorf("no window may open: %v", *urls)
	}
}

func TestOpenChatReplayStopsTheServerWhenTheWindowFails(t *testing.T) {
	old := openChatWindow
	t.Cleanup(func() { openChatWindow = old })

	var url string
	openChatWindow = func(u string, _ config.Config) error { url = u; return errors.New("no browser found") }

	m := New("test")
	m.cfg.Chat.Enabled = true
	if err := m.openChatReplay(vodWithChat(), filepath.Join(t.TempDir(), "x.sock")); err == nil || m.chatOpened["abcdefghijk"] {
		t.Fatalf("the error must come back: %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Error("the server must stop when no window shows it")
	}
}

func TestPlayerExitForgetsTheSocketAndAllowsTheChatAgain(t *testing.T) {
	m := New("test")
	m.playing["abcdefghijk"] = "A VOD"
	m.sockets["abcdefghijk"] = "/tmp/x.sock"
	m.chatOpened["abcdefghijk"] = true

	m.Update(playerExitedMsg{ids: []string{"abcdefghijk"}})
	if m.sockets["abcdefghijk"] != "" || m.chatOpened["abcdefghijk"] {
		t.Error("a new playback of the same video needs a new socket and a new chat window")
	}
}
