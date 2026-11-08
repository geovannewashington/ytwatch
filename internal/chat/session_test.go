package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

func msg(id string, seconds float64, text string) youtube.ChatMessage {
	m := youtube.ChatMessage{ID: id, Author: "@" + id, Text: text, Offset: time.Duration(seconds * float64(time.Second))}
	if text != "" {
		m.Runs = []youtube.ChatRun{{Text: text}}
	}

	return m
}

// text is what a message of the page says, without its pictures.
func (m Message) text() string {
	var b strings.Builder
	for _, run := range m.Runs {
		b.WriteString(run.Text)
	}

	return b.String()
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
	mu     sync.Mutex
	pos    time.Duration
	paused bool
	speed  float64
	err    error
	now    time.Time
	calls  []fetchCall
	pages  []youtube.ChatPage
	fail   error
}

func newFake(t *testing.T) *fakeSession {
	t.Helper()

	f := &fakeSession{now: time.Now(), speed: 1}
	f.Session = NewSession(Options{
		Title: "A stream", Token: "first", Socket: "unused",
		State: func() (player.State, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return player.State{Time: f.pos, Paused: f.paused, Speed: f.speed}, f.err
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
	if len(snapshot.Messages) != 3 || snapshot.Messages[0].text() != "a" || snapshot.Messages[2].text() != "c" {
		t.Fatalf("messages up to 100 s, without the old one: %+v", snapshot.Messages)
	}
	if snapshot.Time != 100 || snapshot.Status != "" || snapshot.Title != "A stream" {
		t.Errorf("snapshot = %+v", snapshot)
	}

	f.step(135 * time.Second) // the video moved on
	if got := f.Snapshot(50); len(got.Messages) != 4 || got.Messages[3].text() != "d" {
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
	if got := f.Snapshot(50); len(got.Messages) != 1 || got.Messages[0].text() != "b" {
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
	if f.Ended() {
		t.Fatal("one failed poll is not the end")
	}

	f.mu.Lock()
	f.now = f.now.Add(failGrace + time.Second)
	f.mu.Unlock()
	f.step(0)
	if got := f.Snapshot(5); !f.Ended() || !got.Ended || got.Status != "Playback ended" {
		t.Errorf("a socket that keeps failing ends the chat: %+v", got)
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
	if len(got.Messages) != 2 || got.Messages[0].text() != "three" || got.Messages[1].text() != "four" {
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

	time.Sleep(20 * time.Millisecond)
	g.mu.Lock()
	g.now = g.now.Add(2 * startupGrace) // mpv had its time to start
	g.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must stop when mpv is gone")
	}
}

func TestSessionWaitsForMpvToOpenItsSocket(t *testing.T) {
	f := newFake(t)
	f.err = errors.New("connect: no such file or directory") // mpv is still starting

	for range 5 {
		f.step(0)
	}
	if f.Ended() {
		t.Fatal("a socket that is not there yet is not the end of the playback")
	}
	if got := f.Snapshot(5).Status; got != "Waiting for the video to start..." {
		t.Errorf("status = %q", got)
	}

	f.mu.Lock()
	f.err = nil
	f.pos = 10 * time.Second
	f.mu.Unlock()
	f.step(10 * time.Second)
	if f.Ended() || f.Snapshot(5).Ended {
		t.Fatal("mpv is there now")
	}

	// Once mpv has answered, a socket that stays lost is the end.
	f.mu.Lock()
	f.err = errors.New("connection refused")
	f.mu.Unlock()
	f.step(10 * time.Second)
	if f.Ended() {
		t.Fatal("one failed poll is not the end")
	}

	f.mu.Lock()
	f.now = f.now.Add(failGrace + time.Second)
	f.mu.Unlock()
	f.step(10 * time.Second)
	if !f.Ended() {
		t.Error("a socket that was there and stays gone means mpv ended")
	}
}

func TestSessionSurvivesAFailedPollWhileMpvIsBusy(t *testing.T) {
	f := newFake(t)
	f.step(100 * time.Second) // mpv answers

	for range 3 { // then it does not, for a moment: it opens the stream
		f.mu.Lock()
		f.err = errors.New("read unix: i/o timeout")
		f.now = f.now.Add(2 * time.Second)
		f.mu.Unlock()
		f.step(100 * time.Second)
	}
	if f.Ended() {
		t.Fatal("a few seconds of silence is not the end")
	}

	f.mu.Lock()
	f.err = nil
	f.now = f.now.Add(time.Hour) // a long time later, but mpv answered in between
	f.mu.Unlock()
	f.step(101 * time.Second)

	f.mu.Lock()
	f.err = errors.New("read unix: i/o timeout")
	f.now = f.now.Add(2 * time.Second)
	f.mu.Unlock()
	f.step(101 * time.Second)
	if f.Ended() {
		t.Error("an answer starts the count again")
	}
}

func TestSessionEndsAtOnceWhenToldSo(t *testing.T) {
	f := newFake(t)
	f.step(100 * time.Second)

	f.End()
	if got := f.Snapshot(5); !f.Ended() || got.Status != "Playback ended" {
		t.Errorf("End must not wait for the socket: %+v", got)
	}
}

func TestSessionGivesUpWhenMpvNeverOpensItsSocket(t *testing.T) {
	f := newFake(t)
	f.err = errors.New("connect: no such file or directory")

	f.step(0)
	if f.Ended() {
		t.Fatal("too early to give up")
	}

	f.mu.Lock()
	f.now = f.now.Add(startupGrace + time.Second)
	f.mu.Unlock()
	f.step(0)
	if !f.Ended() {
		t.Error("mpv that never starts must end the chat, or the page waits for ever")
	}
}

func TestSnapshotIncludesTheNextMomentsForThePageClock(t *testing.T) {
	f := newFake(t)
	f.pages = []youtube.ChatPage{{
		Messages: []youtube.ChatMessage{msg("now", 99, "now"), msg("soon", 101.5, "soon"), msg("later", 103, "later")},
		Next:     "t",
	}}
	f.step(100 * time.Second)

	got := f.Snapshot(10).Messages
	if len(got) != 2 || got[1].text() != "soon" {
		t.Errorf("up to ahead (%v) of the time: %+v", ahead, got)
	}
}

func TestSnapshotCarriesWhatTheRichMessageHas(t *testing.T) {
	f := newFake(t)
	rich := youtube.ChatMessage{
		ID: "r", Offset: time.Second, Author: "@mod", Role: "moderator", Kind: youtube.ChatPaid, Amount: "$5.00", Sub: "hi",
		Photo:  "https://yt4.ggpht.com/p=s64",
		Badges: []youtube.ChatBadge{{Kind: "member", Label: "Member (1 year)", Image: "https://yt3.ggpht.com/b"}, {Kind: "moderator", Label: "Moderator"}},
		Runs:   []youtube.ChatRun{{Text: "hello "}, {Image: "https://fonts.gstatic.com/s/e/notoemoji/15.1/1f4a5/72.png", Alt: ":boom:"}},
		Colors: youtube.ChatColors{HeaderBackground: "rgba(1,2,3,1.00)", BodyText: "rgba(0,0,0,0.87)"},
	}
	f.pages = []youtube.ChatPage{{Messages: []youtube.ChatMessage{rich}, Next: "t"}}
	f.step(10 * time.Second)

	got := f.Snapshot(5).Messages[0]
	if got.Photo != rich.Photo || got.Role != "moderator" || got.Kind != 1 || got.Amount != "$5.00" || got.Sub != "hi" ||
		len(got.Badges) != 2 || got.Badges[0].Image != "https://yt3.ggpht.com/b" || got.Badges[1].Image != "" ||
		len(got.Runs) != 2 || got.Runs[1].Alt != ":boom:" || got.Runs[1].Image == "" ||
		got.Colors == nil || got.Colors.HeaderBackground != "rgba(1,2,3,1.00)" || got.Colors.BodyBackground != "" {
		t.Errorf("message = %+v", got)
	}
}

func TestSnapshotNeverPassesAPictureFromAnotherPlace(t *testing.T) {
	m := youtube.ChatMessage{
		ID: "x", Author: "@a", Photo: "https://evil.example/p.png",
		Badges: []youtube.ChatBadge{{Kind: "member", Image: "http://yt3.ggpht.com/b"}},
		Runs:   []youtube.ChatRun{{Image: "https://yt3.ggpht.com.evil.example/e.png", Alt: ":e:"}},
	}

	got := page(m)
	if got.Photo != "" || got.Badges[0].Image != "" || got.Runs[0].Image != "" || got.Runs[0].Alt != ":e:" {
		t.Errorf("the page must get no address that YouTube's servers do not own: %+v", got)
	}
}

func TestSnapshotTellsThePageWhetherTheVideoMoves(t *testing.T) {
	f := newFake(t)

	f.step(10 * time.Second)
	if got := f.Snapshot(5); got.Paused || got.Speed != 1 || got.Time != 10 {
		t.Errorf("playing: %+v", got)
	}

	f.mu.Lock()
	f.paused, f.speed = true, 2
	f.mu.Unlock()
	f.step(10 * time.Second)
	if got := f.Snapshot(5); !got.Paused || got.Speed != 2 {
		t.Errorf("paused at double speed: %+v", got)
	}

	if got := newFake(t).Snapshot(5); got.Speed != 1 {
		t.Errorf("before the first answer the speed is 1, got %v", got.Speed)
	}
}
