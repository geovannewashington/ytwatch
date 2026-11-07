package chat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

func msg(id string, seconds float64, text string) youtube.ChatMessage {
	return youtube.ChatMessage{ID: id, Author: "@" + id, Text: text, Offset: time.Duration(seconds * float64(time.Second))}
}

type fetchCall struct {
	token  string
	offset time.Duration
	seek   bool
}

// fakeSession is a Session whose mpv and YouTube are made up. Pages are given
// by the test for each fetch; every call is recorded.
type fakeSession struct {
	*Session
	mu    sync.Mutex
	pos   time.Duration
	err   error
	now   time.Time
	calls []fetchCall
	pages []youtube.ChatPage
	fail  error
}

func newFake(t *testing.T) *fakeSession {
	t.Helper()

	f := &fakeSession{now: time.Now()}
	f.Session = NewSession(Options{
		Title: "A stream", Token: "first", Socket: "unused",
		Position: func() (time.Duration, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.pos, f.err
		},
		Fetch: func(_ context.Context, token string, offset time.Duration, seek bool) (youtube.ChatPage, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls = append(f.calls, fetchCall{token, offset, seek})
			if f.fail != nil {
				return youtube.ChatPage{}, f.fail
			}
			if len(f.pages) == 0 {
				return youtube.ChatPage{}, nil
			}
			page := f.pages[0]
			f.pages = f.pages[1:]
			return page, nil
		},
		Now: func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
	})

	return f
}

// step is one turn of Run: ask mpv, then wait for the fetch it started.
func (f *fakeSession) step(pos time.Duration) {
	f.mu.Lock()
	f.pos = pos
	f.mu.Unlock()
	f.Session.step(context.Background())
	f.Session.wg.Wait()
}

func (f *fakeSession) calledWith() []fetchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fetchCall(nil), f.calls...)
}

func TestSessionStartsAtTheTimeOfTheVideoAndFollowsIt(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{
		{Messages: []youtube.ChatMessage{msg("old", 10, "before"), msg("a", 85, "a"), msg("b", 90, "b")}, Next: "t2"},
		{Messages: []youtube.ChatMessage{msg("c", 100, "c"), msg("d", 130, "d")}, Next: "t3"},
		{Messages: []youtube.ChatMessage{msg("e", 160, "e")}, Next: "t4"},
	}

	f.step(100 * time.Second) // asks for the time of the video, 20 s of history
	f.step(100 * time.Second) // 90 s fetched: less than 40 s ahead
	f.step(100 * time.Second) // 130 s fetched: still short of 140 s
	f.step(100 * time.Second) // 160 s fetched: enough

	want := []fetchCall{{"first", 80 * time.Second, true}, {"t2", 0, false}, {"t3", 0, false}}
	got := f.calledWith()
	if len(got) != len(want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	snapshot := f.Snapshot(50)
	if len(snapshot.Messages) != 3 || snapshot.Messages[0].Text != "a" || snapshot.Messages[2].Text != "c" {
		t.Fatalf("messages up to 100 s, without the old one: %+v", snapshot.Messages)
	}
	if snapshot.Time != 100 || snapshot.Status != "" || snapshot.Title != "A stream" {
		t.Errorf("snapshot = %+v", snapshot)
	}

	f.step(135 * time.Second) // the video moved on
	if got := f.Snapshot(50); len(got.Messages) != 4 || got.Messages[3].Text != "d" {
		t.Errorf("after moving on: %+v", got.Messages)
	}
}

func TestSessionStartsAgainAfterASeek(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{
		{Messages: []youtube.ChatMessage{msg("a", 100, "a")}, Next: "t2"},
		{Messages: []youtube.ChatMessage{msg("z", 1000, "z")}, Next: "t3"},
		{Messages: []youtube.ChatMessage{msg("b", 30, "b")}, Next: "t4"},
	}

	f.step(100 * time.Second)
	f.step(1000 * time.Second) // a big jump forward
	f.step(40 * time.Second)   // and back before what was fetched

	calls := f.calledWith()
	if len(calls) != 3 || !calls[1].seek || calls[1].token != "first" || calls[1].offset != 980*time.Second ||
		!calls[2].seek || calls[2].offset != 20*time.Second {
		t.Fatalf("each jump must ask again at its time: %+v", calls)
	}
	if got := f.Snapshot(50); len(got.Messages) != 1 || got.Messages[0].Text != "b" {
		t.Errorf("only the messages of the new place: %+v", got.Messages)
	}
}

func TestSessionStopsAtTheEndOfTheChat(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{{Messages: []youtube.ChatMessage{msg("a", 5, "a")}}} // no next token

	f.step(10 * time.Second)
	f.step(10 * time.Second)
	f.step(500 * time.Second)

	if calls := f.calledWith(); len(calls) != 1 {
		t.Errorf("nothing more to ask after the last page: %+v", calls)
	}
}

func TestSessionCountsAnEmptyPageSoAQuietChatDoesNotLoop(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{{Next: "t2"}, {Next: "t3"}, {Next: "t4"}, {Next: "t5"}, {Next: "t6"}, {Next: "t7"},
		{Next: "t8"}, {Next: "t9"}, {Next: "t10"}, {Next: "t11"}, {Next: "t12"}}

	for range 30 {
		f.step(100 * time.Second)
	}

	// 20 s of history, then 5 s per empty page up to 40 s past 100 s.
	if calls := f.calledWith(); len(calls) > 14 {
		t.Errorf("a quiet chat made %d requests", len(calls))
	}
}

func TestSessionWaitsAfterAnError(t *testing.T) {
	f := newFake(t)
	f.fail = errors.New("offline")

	f.step(50 * time.Second)
	f.step(50 * time.Second)
	if calls := f.calledWith(); len(calls) != 1 {
		t.Fatalf("no retry before the wait is over: %d calls", len(calls))
	}
	if got := f.Snapshot(5); got.Status == "" {
		t.Error("the page should say it is trying again")
	}

	f.mu.Lock()
	f.now = f.now.Add(retryAfter + time.Second)
	f.fail = nil
	f.pages = []youtube.ChatPage{{Messages: []youtube.ChatMessage{msg("a", 45, "a")}, Next: "t"}}
	f.mu.Unlock()

	f.step(50 * time.Second)
	if calls := f.calledWith(); len(calls) != 2 {
		t.Errorf("should retry after the wait: %d calls", len(calls))
	}
	if got := f.Snapshot(5); got.Status != "" || len(got.Messages) != 1 {
		t.Errorf("a good page clears the error: %+v", got)
	}
}

func TestSessionReportsWhatItWaitsFor(t *testing.T) {
	f := newFake(t)
	if got := f.Snapshot(5).Status; got != "Waiting for the video to start..." {
		t.Errorf("before mpv has a time: %q", got)
	}

	f.mu.Lock()
	f.err = player.ErrNoPlaybackTime
	f.mu.Unlock()
	f.step(0)
	if f.Ended() {
		t.Error("mpv that has no time yet is not gone")
	}

	f.mu.Lock()
	f.err = errors.New("connection refused")
	f.mu.Unlock()
	f.step(0)
	if got := f.Snapshot(5); !f.Ended() || !got.Ended || got.Status != "Playback ended" {
		t.Errorf("a missing socket ends the chat: %+v", got)
	}
}

func TestSessionSnapshotLimitsAndSkipsEmptyMessages(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{{
		Messages: []youtube.ChatMessage{msg("a", 1, "one"), msg("b", 2, ""), msg("c", 3, "three"), msg("d", 4, "four"), msg("e", 999, "future")},
		Next:     "t",
	}}
	f.step(10 * time.Second)

	got := f.Snapshot(2)
	if len(got.Messages) != 2 || got.Messages[0].Text != "three" || got.Messages[1].Text != "four" {
		t.Errorf("the newest two that were said already: %+v", got.Messages)
	}
	if all := f.Snapshot(50); len(all.Messages) != 3 {
		t.Errorf("an empty message is skipped: %+v", all.Messages)
	}
}

func TestSessionTrimsALongChat(t *testing.T) {
	f := newFake(t)
	page := youtube.ChatPage{Next: "t"}
	for i := range maxKept + 10 {
		page.Messages = append(page.Messages, youtube.ChatMessage{ID: time.Duration(i).String(), Offset: time.Duration(i) * time.Second})
	}
	f.pages = []youtube.ChatPage{page}
	f.step(0)

	f.Session.mu.Lock()
	defer f.Session.mu.Unlock()
	if len(f.messages) != keepAfter || f.from != f.messages[0].Offset {
		t.Fatalf("kept %d messages, from = %v", len(f.messages), f.from)
	}
}

func TestRunStopsWhenTheContextEndsOrMpvIsGone(t *testing.T) {
	f := newFake(t)
	f.Session.opts.Every = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must stop when the context ends")
	}

	g := newFake(t)
	g.err = errors.New("gone")
	g.Session.opts.Every = time.Millisecond
	done = make(chan struct{})
	go func() { g.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must stop when mpv is gone")
	}
}
