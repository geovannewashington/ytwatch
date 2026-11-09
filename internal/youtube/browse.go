package youtube

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// BrowsePageSize is how many entries of a playlist or a channel tab one request
// lists. The next page is asked for when the list is scrolled near its end.
const BrowsePageSize = 100

// Browse lists the videos of the playlist at url, in the playlist's order. It
// sends the same events as Search: a result for each video, then the search and
// details end events.
func Browse(ctx context.Context, url string, loc locale.Locale, emit func(Event)) {
	BrowseFrom(ctx, url, 1, loc, emit)
}

// BrowseFrom lists one page of the playlist at url, starting at the entry
// start, counted from 1. The ranks continue from the entries before start.
func BrowseFrom(ctx context.Context, url string, start int, loc locale.Locale, emit func(Event)) {
	browseList(ctx, errNoVideos, emit, func(
		ctx context.Context,
		seen *sync.Map,
		count *atomic.Int64,
		emit func(Event),
	) queryResult {
		return runQueryFrom(ctx, url, start, start+BrowsePageSize-1, loc, mainRanking, false, nil, nil, seen, count, emit)
	})
}

// Tabs of a channel, in the order the interface shows them.
const (
	ChannelVideos = iota
	ChannelShorts
	ChannelLive
	ChannelPodcasts
	ChannelPlaylists
)

// ChannelTabs are the names of the channel tabs, indexed by the constants above.
var ChannelTabs = [...]string{"Videos", "Shorts", "Live", "Podcasts", "Playlists"}

// channelTabPaths are the page of each tab under the address of the channel.
var channelTabPaths = [...]string{"/videos", "/shorts", "/streams", "/podcasts", "/playlists"}

// listsPlaylists reports whether a tab lists playlists instead of videos.
func listsPlaylists(tab int) bool { return tab == ChannelPodcasts || tab == ChannelPlaylists }

// BrowseChannel lists a tab of the channel at base. It sends the same events as
// Search. The entries do not say their channel, so it is filled in: from the
// page when it says so, otherwise with name.
func BrowseChannel(ctx context.Context, base string, tab int, name string, loc locale.Locale, emit func(Event)) {
	BrowseChannelFrom(ctx, base, tab, 1, name, loc, emit)
}

// BrowseChannelFrom lists one page of a tab of the channel at base, starting at
// the entry start, counted from 1. The ranks continue from the entries before
// start.
func BrowseChannelFrom(ctx context.Context, base string, tab, start int, name string, loc locale.Locale, emit func(Event)) {
	end := start + BrowsePageSize - 1
	source := strings.TrimRight(base, "/") + channelTabPaths[tab]
	empty := errors.New("no " + strings.ToLower(ChannelTabs[tab]) + " found in this channel")

	browseList(ctx, empty, func(e Event) {
		if e.Kind == EventResult {
			if e.Video.PlaylistUploader != "" {
				name = e.Video.PlaylistUploader
			}
			e.Video.Channel = name
			if tab == ChannelPodcasts && e.Video.IsPlaylist() {
				e.Video.PlaylistType = media.PlaylistPodcast
			}
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
		if listsPlaylists(tab) {
			// yt-dlp skips the courses of a channel, so the page tells them. It
			// is read while yt-dlp lists, and the courses come after the
			// playlists, above them in the list.
			var courses chan []media.Video
			if tab == ChannelPlaylists && start <= 1 {
				courses = make(chan []media.Video, 1)
				go func() { courses <- fetchCourses(ctx, source, loc) }()
			}
			result = runEntryQueryFrom(ctx, source, start, end, loc, mainRanking, media.Video.IsPlaylist, seen, count, emit)
			if courses != nil {
				emitCourses(<-courses, seen, count, emit)
			}
		} else {
			result = runQueryFrom(ctx, source, start, end, loc, mainRanking, false, nil, nil, seen, count, emit)
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
	// Handle is the @handle of the channel, "" when it has none.
	Handle string
	// Description is the whole description of the channel, which a search
	// result only has the start of.
	Description string
	// Tabs are the tabs the channel has, in the order of ChannelTabs.
	Tabs []int
}

// channelData is the part of ytInitialData of a channel page that ytwatch uses.
type channelData struct {
	Metadata struct {
		Channel struct {
			Title      string `json:"title"`
			ExternalID string `json:"externalId"`
			VanityURL  string `json:"vanityChannelUrl"`
			Desc       string `json:"description"`
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
	info := ChannelInfo{
		ID:     data.Metadata.Channel.ExternalID,
		Name:   data.Metadata.Channel.Title,
		Handle: HandleOf(data.Metadata.Channel.VanityURL),

		Description: data.Metadata.Channel.Desc,
	}
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
	emit(Event{Kind: EventSearchDone, Count: int(count.Load()), Err: err, More: result.listed >= BrowsePageSize})
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

// HandleOf is the @handle at the end of a channel address, or "". A handle with
// letters outside ASCII comes percent-encoded in an address.
func HandleOf(address string) string {
	if i := strings.LastIndex(address, "/"); i >= 0 {
		address = address[i+1:]
	}
	if unescaped, err := url.PathUnescape(address); err == nil {
		address = unescaped
	}
	if shownHandle.MatchString(address) {
		return address
	}

	return ""
}

// shownHandle is the form of a handle to show. It is looser than a link's
// handle, which is checked before it goes into an address.
var shownHandle = regexp.MustCompile(`^@[\p{L}\p{N}._-]{1,100}$`)

// fetchCourses reads the courses of the channel page at source. It is nil when
// the page cannot be read: the playlists are still listed.
func fetchCourses(ctx context.Context, source string, loc locale.Locale) []media.Video {
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()

	pageURL := source
	if params := loc.PageParams(); params != "" {
		pageURL += "?" + params
	}
	page, err := fetchResultsPage(ctx, pageURL, loc)
	if err != nil {
		return nil
	}

	return parseCourses(page)
}

// emitCourses sends courses as playlists, ranked before the first playlist, as
// on YouTube.
func emitCourses(
	courses []media.Video,
	seen *sync.Map,
	count *atomic.Int64,
	emit func(Event),
) {
	for i, course := range courses {
		if _, duplicate := seen.LoadOrStore(course.ID, struct{}{}); duplicate {
			continue
		}
		emit(Event{
			Kind:  EventResult,
			Video: course,
			Count: int(count.Add(1)),
			Rank:  i - len(courses), // the first playlist has rank 0
		})
	}
}

// courseID is the form of a playlist ID, which is safe to put in an address.
var courseID = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)

// parseCourses reads the courses of a channel page. A course is a playlist, but
// yt-dlp does not know its kind of entry.
func parseCourses(page []byte) []media.Video {
	var data any
	if found, err := decodeAfter(page, initialDataMarker, &data); err != nil || !found {
		return nil
	}

	var courses []media.Video
	walkObjects(data, func(object map[string]any) {
		if lockup, ok := object["lockupViewModel"].(map[string]any); ok {
			if course, ok := courseOf(lockup); ok {
				courses = append(courses, course)
			}
		}
	})

	return courses
}

// courseOf reads a lockupViewModel that is a course.
func courseOf(lockup map[string]any) (media.Video, bool) {
	if kind, _ := lockup["contentType"].(string); kind != "LOCKUP_CONTENT_TYPE_COURSE" {
		return media.Video{}, false
	}
	id, _ := lockup["contentId"].(string)
	if !courseID.MatchString(id) {
		return media.Video{}, false
	}

	title := "Untitled course"
	if meta, ok := lockup["metadata"].(map[string]any); ok {
		if view, ok := meta["lockupMetadataViewModel"].(map[string]any); ok {
			if t, ok := view["title"].(map[string]any); ok {
				if content, _ := t["content"].(string); content != "" {
					title = content
				}
			}
		}
	}

	address := "https://www.youtube.com/playlist?list=" + id
	course := media.Video{
		ID: id, Title: title, URL: address, WebpageURL: address,
		PlaylistType: media.PlaylistCourse,
	}

	// The picture and the "10 lessons" badge are in the thumbnail view model.
	thumbnail := dig(lockup, "contentImage", "collectionThumbnailViewModel", "primaryThumbnail", "thumbnailViewModel")
	if sources, ok := dig(thumbnail, "image", "sources").([]any); ok && len(sources) > 0 {
		if first, ok := sources[0].(map[string]any); ok {
			if raw, _ := first["url"].(string); isSafeImage(raw) {
				course.Thumbnail = raw
			}
		}
	}
	if overlays, ok := dig(thumbnail, "overlays").([]any); ok {
		for _, overlay := range overlays {
			badges, _ := dig(overlay, "thumbnailOverlayBadgeViewModel", "thumbnailBadges").([]any)
			for _, badge := range badges {
				text, _ := dig(badge, "thumbnailBadgeViewModel", "text").(string)
				if words := strings.Fields(text); len(words) > 0 {
					if n, err := strconv.Atoi(words[0]); err == nil && n > 0 {
						course.PlaylistCount = n
					}
				}
			}
		}
	}

	return course, true
}

// maxJSONDepth stops walkObjects in a page nested far deeper than YouTube's.
const maxJSONDepth = 100

// walkObjects calls visit for every object in decoded JSON, an object before
// the objects inside it, and the entries of an array in order.
func walkObjects(node any, visit func(map[string]any)) {
	walkObjectsAt(node, 0, visit)
}

func walkObjectsAt(node any, depth int, visit func(map[string]any)) {
	if depth > maxJSONDepth {
		return
	}

	switch n := node.(type) {
	case map[string]any:
		visit(n)
		for _, child := range n {
			walkObjectsAt(child, depth+1, visit)
		}
	case []any:
		for _, child := range n {
			walkObjectsAt(child, depth+1, visit)
		}
	}
}

// dig reads the value at the path of keys in decoded JSON, nil when a step is
// missing.
func dig(node any, keys ...string) any {
	for _, key := range keys {
		object, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = object[key]
	}

	return node
}

// isSafeImage reports whether raw is an https address on YouTube's image hosts.
func isSafeImage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := u.Hostname()

	return host == "i.ytimg.com" || strings.HasSuffix(host, ".ytimg.com") ||
		strings.HasSuffix(host, ".ggpht.com") || strings.HasSuffix(host, ".googleusercontent.com")
}

// runChannelQuery lists the channels of a results page. yt-dlp does not tell
// the official artist channels, which the page marks, so the page is read too.
func runChannelQuery(
	ctx context.Context,
	source string,
	start, end int,
	loc locale.Locale,
	seen *sync.Map,
	count *atomic.Int64,
	emit func(Event),
) queryResult {
	artists := make(chan map[string]bool, 1)
	go func() {
		if start > 1 {
			artists <- nil // the page has the first channels only
			return
		}
		artists <- fetchArtistChannels(ctx, source, loc)
	}()

	var artistSet map[string]bool
	waited := false

	return runEntryQueryFrom(ctx, source, start, end, loc, mainRanking, media.Video.IsChannel, seen, count, func(e Event) {
		if e.Kind == EventResult {
			if !waited {
				artistSet, waited = <-artists, true
			}
			if artistSet[e.Video.ID] {
				e.Video.ChannelArtist = true
			}
		}
		emit(e)
	})
}

// fetchArtistChannels reads the results page at source, for the IDs of the
// channels it marks as official artists. It is nil when the page cannot be read.
func fetchArtistChannels(ctx context.Context, source string, loc locale.Locale) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second) // it holds back the first channel
	defer cancel()

	pageURL := source
	if params := loc.PageParams(); params != "" {
		pageURL += "&" + params
	}
	page, err := fetchResultsPage(ctx, pageURL, loc)
	if err != nil {
		return nil
	}

	return parseArtistChannels(page)
}

// parseArtistChannels reads the IDs of the channels a results page marks with
// the official artist badge.
func parseArtistChannels(page []byte) map[string]bool {
	var data any
	if found, err := decodeAfter(page, initialDataMarker, &data); err != nil || !found {
		return nil
	}

	artists := map[string]bool{}
	walkObjects(data, func(object map[string]any) {
		renderer, ok := object["channelRenderer"].(map[string]any)
		if !ok {
			return
		}
		id, _ := renderer["channelId"].(string)
		badges, _ := renderer["ownerBadges"].([]any)
		for _, badge := range badges {
			if style, _ := dig(badge, "metadataBadgeRenderer", "style").(string); style == "BADGE_STYLE_TYPE_VERIFIED_ARTIST" && id != "" {
				artists[id] = true
			}
		}
	})

	return artists
}
