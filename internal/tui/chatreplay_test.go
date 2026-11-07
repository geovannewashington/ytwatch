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

	socket := filepath.Join(t.TempDir(), "starting.sock") // mpv has not opened its socket yet
	if err := m.openChatReplay(vodWithChat(), socket); err != nil {
		t.Fatal(err)
	}
	if len(*urls) != 1 || !strings.HasPrefix((*urls)[0], "http://127.0.0.1:") || !m.chatOpened["abcdefghijk"] {
		t.Fatalf("urls = %v, opened = %v", *urls, m.chatOpened)
	}

	messages := func() string {
		response, err := http.Get((*urls)[0] + "messages")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)

		return string(data)
	}

	// The window gets a page, which waits for mpv instead of giving up.
	if body := messages(); !strings.Contains(body, "Waiting for the video to start") || !strings.Contains(body, "A VOD") {
		t.Errorf("messages = %s", body)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(messages(), "Playback ended") {
		t.Error("a socket that is not there yet is not the end of the playback")
	}

	m.chats["abcdefghijk"].End()
	if body := messages(); !strings.Contains(body, "Playback ended") {
		t.Errorf("after End: %s", body)
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

func TestPlayerExitEndsTheChatAndAllowsANewOne(t *testing.T) {
	fakeChatWindow(t)
	m := New("test")
	m.cfg.Chat.Enabled = true
	m.playing["abcdefghijk"] = "A VOD"
	m.sockets["abcdefghijk"] = filepath.Join(t.TempDir(), "x.sock")

	if err := m.openChatReplay(vodWithChat(), m.sockets["abcdefghijk"]); err != nil {
		t.Fatal(err)
	}
	session := m.chats["abcdefghijk"]
	if session == nil || session.Ended() {
		t.Fatal("the chat must be running")
	}

	m.Update(playerExitedMsg{ids: []string{"abcdefghijk"}})
	if m.sockets["abcdefghijk"] != "" || m.chatOpened["abcdefghijk"] || m.chats["abcdefghijk"] != nil {
		t.Error("a new playback of the same video needs a new socket and a new chat window")
	}
	if !session.Ended() {
		t.Error("the page must be told that mpv is gone, without waiting for the socket")
	}

	m.cancel()
}
