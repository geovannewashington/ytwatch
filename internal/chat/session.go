// Package chat shows the chat replay of a past stream next to mpv.
//
// YouTube's own replay window shows a message only when a player tells it the
// time of the video, so a window without a player stays empty. Here mpv is the
// player: a Session reads its time from mpv's control socket, fetches the
// messages that belong to that time a little ahead, and a Server shows them in
// a page that the browser window loads.
package chat

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

const (
	pollEvery = 700 * time.Millisecond

	// lookahead is how far ahead of the video the messages are fetched.
	lookahead = 40 * time.Second

	// history is how much chat from before a jump is fetched, so the page is
	// not empty right after a seek.
	history = 20 * time.Second

	// jump is how far past the fetched messages the video may be before the
	// chat starts again at its time, instead of paging through all of it.
	jump = 60 * time.Second

	// emptyPage is how much time an empty page counts for, so a quiet chat
	// does not make the same request over and over.
	emptyPage = 5 * time.Second

	retryAfter = 5 * time.Second
	maxKept    = 4000
	keepAfter  = 2500
)

// end is "to the end of the chat": there is no page after the last one.
const end = time.Duration(math.MaxInt64 / 2)

// Options are the inputs of a Session. Title, Token and Socket are needed. The
// functions are for tests.
type Options struct {
	Title  string
	Token  string // the token of the watch page, which opens the replay
	Socket string // mpv's control socket

	Position func() (time.Duration, error)
	Fetch    func(ctx context.Context, token string, offset time.Duration, seek bool) (youtube.ChatPage, error)
	Now      func() time.Time
	Every    time.Duration
}

// Session follows one playback of a past stream.
type Session struct {
	opts Options
	wg   sync.WaitGroup

	mu       sync.Mutex
	messages []youtube.ChatMessage // sorted by Offset
	seen     map[string]bool
	next     string        // the token of the page after the last one
	from, to time.Duration // the part of the video the messages cover
	started  bool          // a first page was asked for
	loading  bool
	retryAt  time.Time
	err      string

	pos     time.Duration
	havePos bool
	ended   bool // mpv is gone
}

// NewSession returns a Session. Run it with Run.
func NewSession(opts Options) *Session {
	if opts.Position == nil {
		socket := opts.Socket
		opts.Position = func() (time.Duration, error) { return player.PlaybackTime(socket) }
	}
	if opts.Fetch == nil {
		opts.Fetch = youtube.FetchChat
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Every <= 0 {
		opts.Every = pollEvery
	}

	return &Session{opts: opts, seen: map[string]bool{}}
}

// Run follows mpv until ctx ends or mpv is gone.
func (s *Session) Run(ctx context.Context) {
	defer s.wg.Wait()

	ticker := time.NewTicker(s.opts.Every)
	defer ticker.Stop()

	for {
		s.step(ctx)

		if s.Ended() {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type action int

const (
	wait     action = iota
	seek            // start again at the time of the video
	nextPage        // the page after the last one
)

// step asks mpv for the time, and starts a fetch when the messages do not
// reach far enough ahead of it.
func (s *Session) step(ctx context.Context) {
	pos, err := s.opts.Position()

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case errors.Is(err, player.ErrNoPlaybackTime):
		return // mpv is still opening the stream
	case err != nil:
		s.ended = true
		return
	}
	s.pos, s.havePos = pos, true

	var token string
	var offset time.Duration
	var isSeek bool

	switch s.plan(pos) {
	case seek:
		offset, token, isSeek = s.restart(pos), s.opts.Token, true
	case nextPage:
		token = s.next
	default:
		return
	}

	s.loading = true
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		page, err := s.opts.Fetch(ctx, token, offset, isSeek)
		s.accept(page, err, isSeek)
	}()
}

// plan decides what to fetch for a video at pos. The caller holds the lock.
func (s *Session) plan(pos time.Duration) action {
	switch {
	case s.loading || s.opts.Now().Before(s.retryAt):
		return wait
	case !s.started || pos < s.from || pos > s.to+jump:
		return seek
	case s.next != "" && s.to < pos+lookahead:
		return nextPage
	}

	return wait
}

// restart forgets the messages, for a first page or after a jump. It returns
// the time the new page starts at.
func (s *Session) restart(pos time.Duration) time.Duration {
	s.messages = nil
	s.seen = map[string]bool{}
	s.next = ""
	s.started = true
	s.from = max(0, pos-history)
	s.to = s.from

	return s.from
}

// accept adds the messages of a page. A page after a seek also has messages
// from before the time that was asked for, which are left out.
func (s *Session) accept(page youtube.ChatPage, err error, afterSeek bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loading = false

	if err != nil {
		s.err = err.Error()
		s.retryAt = s.opts.Now().Add(retryAfter)

		// Without a first page there is no token for a next one, so the
		// retry must be a first page too.
		if afterSeek {
			s.started = false
		}

		return
	}
	s.err = ""

	last := time.Duration(-1)
	for _, message := range page.Messages {
		last = max(last, message.Offset)
		if s.seen[message.ID] || (afterSeek && message.Offset < s.from) {
			continue
		}

		s.seen[message.ID] = true
		s.messages = append(s.messages, message)
	}

	byOffset := func(a, b youtube.ChatMessage) int { return int(a.Offset - b.Offset) }
	if !slices.IsSortedFunc(s.messages, byOffset) {
		slices.SortStableFunc(s.messages, byOffset)
	}

	s.next = page.Next
	switch {
	case s.next == "":
		s.to = end
	case last >= 0:
		s.to = max(s.to, last)
	default:
		s.to += emptyPage
	}

	// A long video must not make the messages grow without bound.
	if len(s.messages) > maxKept {
		drop := len(s.messages) - keepAfter
		s.from = s.messages[drop].Offset
		s.messages = slices.Clone(s.messages[drop:])
	}
}

// Ended reports whether mpv is gone.
func (s *Session) Ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.ended
}

// Message is a chat message for the page.
type Message struct {
	Time   float64 `json:"t"` // seconds into the video
	Author string  `json:"a"`
	Text   string  `json:"x"`
	Kind   int     `json:"k"` // see youtube.ChatKind
}

// Snapshot is what the page shows.
type Snapshot struct {
	Title    string    `json:"title"`
	Time     float64   `json:"time"` // seconds into the video
	Ended    bool      `json:"ended"`
	Status   string    `json:"status"` // why there is nothing to show, or ""
	Messages []Message `json:"messages"`
}

// Snapshot returns up to limit messages, the newest last, that were said by
// the time mpv is at.
func (s *Session) Snapshot(limit int) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := Snapshot{Title: s.opts.Title, Time: s.pos.Seconds(), Ended: s.ended, Messages: []Message{}}

	// messages is sorted by time, so the ones to show are a prefix.
	shown := sort.Search(len(s.messages), func(i int) bool { return s.messages[i].Offset > s.pos })
	for _, message := range s.messages[max(0, shown-limit):shown] {
		if message.Text == "" && message.Kind == youtube.ChatText {
			continue // only emoji that cannot be drawn
		}

		snapshot.Messages = append(snapshot.Messages, Message{
			Time: message.Offset.Seconds(), Author: message.Author, Text: message.Text, Kind: int(message.Kind),
		})
	}

	switch {
	case s.ended:
		snapshot.Status = "Playback ended"
	case !s.havePos:
		snapshot.Status = "Waiting for the video to start..."
	case s.err != "":
		snapshot.Status = "Could not load the chat, trying again..."
	case len(snapshot.Messages) == 0 && (s.loading || !s.started):
		snapshot.Status = "Loading the chat..."
	case len(snapshot.Messages) == 0:
		snapshot.Status = "No messages yet at this time."
	}

	return snapshot
}
