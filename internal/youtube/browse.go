package youtube

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

const (
	// playlistBrowseLimit is how many videos of a playlist are listed.
	playlistBrowseLimit = 200
	// playlistDetailLimit is how many of them get a detail lookup (description,
	// age). The others show what the playlist listing already has, so a long
	// playlist does not send hundreds of requests to YouTube.
	playlistDetailLimit = 30
)

// Browse lists the videos of the playlist at url, in the playlist's order. It
// sends the same events as Search: a result for each video, then the search and
// details end events.
func Browse(ctx context.Context, url string, loc locale.Locale, emit func(Event)) {
	listCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	jobs := make(chan media.Video, playlistDetailLimit)
	live := make(chan media.Video)
	close(live)
	workers := startDetailWorkers(ctx, jobs, live, loc, emit)

	var seen sync.Map
	var count atomic.Int64
	// Only the first videos get a detail lookup: past that point the jobs are
	// dropped instead of waiting.
	limited := make(chan media.Video)
	go func() {
		sent := 0
		for v := range limited {
			if sent < playlistDetailLimit {
				sent++
				jobs <- v
			}
		}
		close(jobs)
	}()
	result := runQuery(listCtx, url, playlistBrowseLimit, loc, mainRanking, false, nil, &seen, &count, limited, nil, emit)
	close(limited)

	var err error
	switch {
	case result.err != nil && count.Load() == 0:
		err = result.err
	case count.Load() == 0:
		err = errNoVideos
	case listCtx.Err() != nil && ctx.Err() == nil:
		err = errBrowseTimedOut
	}
	emit(Event{Kind: EventSearchDone, Count: int(count.Load()), Err: err})
	workers.Wait()
	emit(Event{Kind: EventDetailsEnd, Count: int(count.Load())})
}

// unavailable reports whether a playlist entry is a video that was removed or
// made private, which cannot be played.
func unavailable(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "[private video]", "[deleted video]", "[unavailable video]":
		return true
	}
	return false
}
