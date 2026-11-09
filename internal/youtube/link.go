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
// fields is set. Channel is the path of the channel page, such as "@handle" or
// "channel/UC...".
type Link struct {
	VideoID    string
	PlaylistID string
	Channel    string
}

var (
	linkVideoID    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	linkPlaylistID = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)
	linkChannelID  = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	linkHandle     = regexp.MustCompile(`^@[\p{L}\p{N}._-]{3,30}$`)
	linkChannelRef = regexp.MustCompile(`^[\p{L}\p{N}._-]{1,100}$`)
)

// channelTabSegments are the pages of a channel that a link may end with. The
// channel opens on its videos whichever one it was.
var channelTabSegments = map[string]bool{
	"featured": true, "videos": true, "shorts": true, "streams": true,
	"live": true, "playlists": true, "community": true, "about": true,
}

// ParseLink reads a link to a video, a playlist or a channel. Anything else,
// including a bare video ID, is not a link: a word such as "programming" has
// the form of an ID, and it must stay a search. A link to a video in a
// playlist is the video. The IDs and names are checked, so they are safe to
// put in a URL.
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
	var tail []string // what follows the channel name
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
		case "channel":
			if len(segments) > 1 && linkChannelID.MatchString(segments[1]) {
				link.Channel, tail = "channel/"+segments[1], segments[2:]
			}
		case "c", "user":
			if len(segments) > 1 && linkChannelRef.MatchString(segments[1]) {
				link.Channel, tail = segments[0]+"/"+url.PathEscape(segments[1]), segments[2:]
			}
		default:
			if linkHandle.MatchString(segments[0]) {
				link.Channel, tail = url.PathEscape(segments[0]), segments[1:]
			}
		}
		if !validChannelTail(tail) {
			link.Channel = ""
		}
	}

	switch {
	case linkVideoID.MatchString(link.VideoID):
		return Link{VideoID: link.VideoID}, true
	case link.VideoID == "" && linkPlaylistID.MatchString(link.PlaylistID):
		return Link{PlaylistID: link.PlaylistID}, true
	case link.VideoID == "" && link.Channel != "":
		return Link{Channel: link.Channel}, true
	}
	return Link{}, false
}

// validChannelTail reports whether the segments after the channel name are
// nothing, or one page of the channel.
func validChannelTail(rest []string) bool {
	return len(rest) == 0 || len(rest) == 1 && channelTabSegments[rest[0]]
}

// PlaylistURL is the playlist page of the link.
func (l Link) PlaylistURL() string {
	return "https://www.youtube.com/playlist?list=" + l.PlaylistID
}

// ChannelURL is the channel page of the link.
func (l Link) ChannelURL() string {
	return "https://www.youtube.com/" + l.Channel
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
