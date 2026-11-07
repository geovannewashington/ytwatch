package youtube

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// Link is a YouTube link typed or pasted in the search line. Only one of the
// IDs is set.
type Link struct {
	VideoID    string
	PlaylistID string
}

var (
	linkVideoID    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	linkPlaylistID = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)
)

// ParseLink reads a link to a video or a playlist. Anything else, including a
// bare video ID, is not a link: a word such as "programming" has the form of
// an ID, and it must stay a search. A link to a video in a playlist is the
// video. The IDs are checked, so they are safe to put in a URL.
func ParseLink(input string) (Link, bool) {
	input = strings.TrimSpace(input)
	if input == "" || strings.ContainsAny(input, " \t\n") {
		return Link{}, false
	}
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}

	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Link{}, false
	}

	host := strings.ToLower(u.Hostname())
	for _, prefix := range []string{"www.", "m.", "music."} {
		host = strings.TrimPrefix(host, prefix)
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")

	var link Link
	switch host {
	case "youtu.be":
		link.VideoID = segments[0]
	case "youtube.com", "youtube-nocookie.com":
		switch segments[0] {
		case "watch":
			link.VideoID = u.Query().Get("v")
		case "playlist":
			link.PlaylistID = u.Query().Get("list")
		case "shorts", "live", "embed":
			if len(segments) > 1 {
				link.VideoID = segments[1]
			}
		}
	}

	switch {
	case linkVideoID.MatchString(link.VideoID):
		return Link{VideoID: link.VideoID}, true
	case link.VideoID == "" && linkPlaylistID.MatchString(link.PlaylistID):
		return Link{PlaylistID: link.PlaylistID}, true
	}
	return Link{}, false
}

// PlaylistURL is the playlist page of the link.
func (l Link) PlaylistURL() string {
	return "https://www.youtube.com/playlist?list=" + l.PlaylistID
}

var errVideoNotFound = errors.New("video not found")

// Lookup shows the video id as the only result. It sends the same events as
// Search.
func Lookup(ctx context.Context, id string, loc locale.Locale, emit func(Event)) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var video media.Video
	err := FetchDetailsBatch(ctx, []media.Video{{ID: id}}, loc, func(v media.Video) {
		if v.ID == id {
			video = v
		}
	})

	count := 0
	if video.ID != "" {
		emit(Event{Kind: EventResult, Video: video, Rank: mainRanking.at(0)})
		count, err = 1, nil
	} else if err == nil {
		err = errVideoNotFound
	}
	emit(Event{Kind: EventSearchDone, Count: count, Err: err})
	emit(Event{Kind: EventDetailsEnd, Count: count})
}
