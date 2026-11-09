package youtube

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// playlistBrowseLimit is how many videos of a playlist are listed.
const playlistBrowseLimit = 200

// Browse lists the videos of the playlist at url, in the playlist's order. It
// sends the same events as Search: a result for each video, then the search and
// details end events.
func Browse(ctx context.Context, url string, loc locale.Locale, emit func(Event)) {
	browseList(ctx, errNoVideos, emit, func(
		ctx context.Context,
		seen *sync.Map,
		count *atomic.Int64,
		emit func(Event),
	) queryResult {
		return runQuery(ctx, url, playlistBrowseLimit, loc, mainRanking, false, nil, nil, seen, count, emit)
	})
}

// Tabs of a channel, in the order the interface shows them.
const (
	ChannelVideos = iota
	ChannelShorts
	ChannelLive
	ChannelPlaylists
)

// ChannelTabs are the names of the channel tabs, indexed by the constants above.
var ChannelTabs = [...]string{"Videos", "Shorts", "Live", "Playlists"}

// channelTabPaths are the page of each tab under the address of the channel.
var channelTabPaths = [...]string{"/videos", "/shorts", "/streams", "/playlists"}

// BrowseChannel lists a tab of the channel at base. It sends the same events as
// Search. The entries do not say their channel, so it is filled in: from the
// page when it says so, otherwise with name.
func BrowseChannel(ctx context.Context, base string, tab int, name string, loc locale.Locale, emit func(Event)) {
	source := strings.TrimRight(base, "/") + channelTabPaths[tab]
	empty := errors.New("no " + strings.ToLower(ChannelTabs[tab]) + " found in this channel")

	browseList(ctx, empty, func(e Event) {
		if e.Kind == EventResult {
			if e.Video.PlaylistUploader != "" {
				name = e.Video.PlaylistUploader
			}
			e.Video.Channel = name
			if e.Video.ChannelURL == "" {
				e.Video.ChannelURL = base
			}
			if e.Video.ChannelID == "" {
				e.Video.ChannelID = e.Video.PlaylistChannelID
			}
		}
		emit(e)
	}, func(ctx context.Context, seen *sync.Map, count *atomic.Int64, emit func(Event)) queryResult {
		var result queryResult
		if tab == ChannelPlaylists {
			result = runEntryQuery(ctx, source, playlistBrowseLimit, loc, mainRanking, media.Video.IsPlaylist, seen, count, emit)
		} else {
			result = runQuery(ctx, source, playlistBrowseLimit, loc, mainRanking, false, nil, nil, seen, count, emit)
		}
		// A channel without Shorts has no Shorts tab, and yt-dlp says so as an
		// error. It is an empty tab.
		if result.err != nil && strings.Contains(result.err.Error(), "does not have a") {
			result.err = nil
		}
		return result
	})
}

// ChannelInfo is what the page of a channel says about it.
type ChannelInfo struct {
	ID   string
	Name string
	// Tabs are the tabs the channel has, in the order of ChannelTabs.
	Tabs []int
}

// channelData is the part of ytInitialData of a channel page that ytwatch uses.
type channelData struct {
	Metadata struct {
		Channel struct {
			Title      string `json:"title"`
			ExternalID string `json:"externalId"`
		} `json:"channelMetadataRenderer"`
	} `json:"metadata"`
	Contents struct {
		Browse struct {
			Tabs []struct {
				Tab struct {
					Endpoint struct {
						CommandMetadata struct {
							Web struct {
								URL string `json:"url"`
							} `json:"webCommandMetadata"`
						} `json:"commandMetadata"`
					} `json:"endpoint"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"twoColumnBrowseResultsRenderer"`
	} `json:"contents"`
}

var errNoChannelPage = errors.New("not a channel page")

// FetchChannelInfo reads the page of the channel at base, to know which tabs
// it has before they are asked for.
func FetchChannelInfo(ctx context.Context, base string, loc locale.Locale) (ChannelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()

	pageURL := base
	if params := loc.PageParams(); params != "" {
		pageURL += "?" + params
	}
	page, err := fetchResultsPage(ctx, pageURL, loc)
	if err != nil {
		return ChannelInfo{}, err
	}
	return parseChannelPage(page)
}

// parseChannelPage reads a channel page. A page that is not one, such as a
// consent page, is an error.
func parseChannelPage(page []byte) (ChannelInfo, error) {
	var data channelData
	found, err := decodeAfter(page, initialDataMarker, &data)
	if err != nil {
		return ChannelInfo{}, err
	}
	if !found || data.Metadata.Channel.ExternalID == "" {
		return ChannelInfo{}, errNoChannelPage
	}

	has := map[string]bool{}
	for _, t := range data.Contents.Browse.Tabs {
		u := t.Tab.Endpoint.CommandMetadata.Web.URL
		if i := strings.LastIndex(u, "/"); i >= 0 {
			has[u[i:]] = true
		}
	}
	info := ChannelInfo{ID: data.Metadata.Channel.ExternalID, Name: data.Metadata.Channel.Title}
	for tab, p := range channelTabPaths {
		if has[p] {
			info.Tabs = append(info.Tabs, tab)
		}
	}
	return info, nil
}

// browseList runs a listing and ends it with the events Search ends with. empty
// is the error when it finds nothing.
func browseList(
	ctx context.Context,
	empty error,
	emit func(Event),
	run func(ctx context.Context, seen *sync.Map, count *atomic.Int64, emit func(Event)) queryResult,
) {
	listCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	var seen sync.Map
	var count atomic.Int64
	result := run(listCtx, &seen, &count, emit)

	var err error
	switch {
	case result.err != nil && count.Load() == 0:
		err = result.err
	case count.Load() == 0:
		err = empty
	case listCtx.Err() != nil && ctx.Err() == nil:
		err = errBrowseTimedOut
	}
	emit(Event{Kind: EventSearchDone, Count: int(count.Load()), Err: err})
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
