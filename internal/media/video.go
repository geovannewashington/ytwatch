package media

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var validVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)

// Video contains the subset of yt-dlp metadata used by ytwatch.
type Video struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	WebpageURL  string `json:"webpage_url"`
	OriginalURL string `json:"original_url"`
	URL         string `json:"url"`
	Channel     string `json:"channel"`
	ChannelURL  string `json:"channel_url"`
	ChannelID   string `json:"channel_id"`
	// ChannelVerified is set for a verified channel, and for an official
	// artist channel, which is verified too.
	ChannelVerified bool `json:"channel_is_verified"`
	// ChannelArtist is set for an official artist channel (the music note),
	// which only the watch page tells.
	ChannelArtist     bool    `json:"-"`
	Uploader          string  `json:"uploader"`
	Duration          float64 `json:"duration"`
	DurationString    string  `json:"duration_string"`
	ViewCount         int64   `json:"view_count"`
	ConcurrentViewers int64   `json:"concurrent_view_count"`
	Timestamp         float64 `json:"timestamp"`
	ReleaseTimestamp  float64 `json:"release_timestamp"`
	UploadDate        string  `json:"upload_date"`
	Description       string  `json:"description"`
	Thumbnail         string  `json:"thumbnail"`
	LiveStatus        string  `json:"live_status"`
	IsLive            bool    `json:"is_live"`
	WasLive           bool    `json:"was_live"`
	MediaType         string  `json:"media_type"`
	Short             bool    `json:"is_short"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	Availability      string  `json:"availability"`
	// Thumbnails lists the sizes a search result offers.
	Thumbnails []ThumbnailRef `json:"thumbnails"`

	// Playlist fields, filled by a detail lookup.
	PlaylistCount int             `json:"playlist_count"`
	ModifiedDate  string          `json:"modified_date"`
	Entries       []PlaylistEntry `json:"entries"`

	// Heatmap is the "most replayed" graph of the video: how much each equal
	// part of it was watched again, from 0 to 1. It is empty for a video without
	// enough views.
	Heatmap []float64 `json:"-"`
	// StoryboardSpec describes the small pictures of the video that the seek bar
	// shows under the mouse, as the watch page gives it. See package storyboard.
	StoryboardSpec string `json:"-"`

	// ChatReplayToken is the continuation token of the chat replay, from the
	// watch page. The replay window cannot open without it.
	ChatReplayToken string `json:"-"`

	// Detailed is set once a detail lookup has been merged in. Search results
	// start without it.
	Detailed bool `json:"-"`
}

// ThumbnailRef is one thumbnail size of a result.
type ThumbnailRef struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PlaylistEntry is one video of a playlist, as listed in a flat lookup.
type PlaylistEntry struct {
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
}

// DurationText is the entry's length such as "3:06", or "" when unknown.
func (e PlaylistEntry) DurationText() string {
	if e.Duration <= 0 {
		return ""
	}
	return FormatDuration(e.Duration)
}

type Kind int

const (
	KindVideo Kind = iota
	KindLive
	KindUpcoming
	KindPastLive
	KindShort
	KindPlaylist
)

// kindNames are the names of the kinds that [search] hide accepts. Plain
// videos cannot be hidden.
var kindNames = map[string]Kind{
	"shorts":    KindShort,
	"playlists": KindPlaylist,
	"live":      KindLive,
	"upcoming":  KindUpcoming,
	"vods":      KindPastLive,
}

// KindNames lists the names ParseKind accepts, for messages.
const KindNames = "shorts, playlists, live, upcoming, vods"

// ParseKind returns the kind with that name (see KindNames).
func ParseKind(name string) (Kind, bool) {
	kind, ok := kindNames[strings.ToLower(strings.TrimSpace(name))]
	return kind, ok
}

// The marks of ChannelBadge are Nerd Font glyphs (nf-fa-music, nf-fa-check_circle),
// so they need a Nerd Font in the terminal.
const (
	artistMark   = "\uf001"
	verifiedMark = "\uf058"
)

// ChannelBadge is the mark next to the name of the channel: a music note for an
// official artist channel, a check for a verified one, or "".
func (v Video) ChannelBadge() string {
	switch {
	case v.ChannelArtist:
		return artistMark
	case v.ChannelVerified:
		return verifiedMark
	}

	return ""
}

func (v Video) ChannelName() string {
	if v.Channel != "" {
		return v.Channel
	}
	if v.Uploader != "" {
		return v.Uploader
	}
	return "Unknown channel"
}

func (v Video) CanonicalURL() string {
	for _, candidate := range []string{v.WebpageURL, v.OriginalURL, v.URL} {
		parsed, err := url.Parse(candidate)
		if err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
			return candidate
		}
	}

	if validVideoID.MatchString(v.ID) {
		return "https://www.youtube.com/watch?v=" + v.ID
	}

	return ""
}

// ThumbnailCandidates lists thumbnail URLs from best to worst; the caller uses
// the first one that works. A plain hq720.jpg is a 1280x720 JPEG but is missing
// for some old videos. The sizes in the search result come next, largest first,
// but those are often WebP. The 320 px mqdefault.jpg always exists and is the
// last resort, because it looks soft once the preview scales it up.
func (v Video) ThumbnailCandidates() []string {
	video := !v.IsPlaylist() && validVideoID.MatchString(v.ID)
	var out []string
	add := func(raw string) {
		if !isHTTPS(raw) || slices.Contains(out, raw) {
			return
		}
		out = append(out, raw)
	}

	if video {
		add("https://i.ytimg.com/vi/" + v.ID + "/hq720.jpg")
	}

	listed := slices.Clone(v.Thumbnails)
	slices.SortStableFunc(listed, func(a, b ThumbnailRef) int { return b.Width - a.Width })
	for _, t := range listed {
		add(t.URL)
	}

	add(v.Thumbnail)
	if video {
		add("https://i.ytimg.com/vi/" + v.ID + "/mqdefault.jpg")
	}

	return out
}

func isHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func (v Video) IsCurrentlyLive() bool {
	return v.IsLive || strings.EqualFold(v.LiveStatus, "is_live")
}

func (v Video) IsUpcoming() bool {
	return strings.EqualFold(v.LiveStatus, "is_upcoming")
}

func (v Video) IsPastLive() bool {
	status := strings.ToLower(v.LiveStatus)
	return !v.IsCurrentlyLive() && (v.WasLive || status == "was_live" || status == "post_live")
}

// HasChatReplay reports whether a past stream has a chat replay that can be
// opened.
func (v Video) HasChatReplay() bool {
	return v.IsPastLive() && v.ChatReplayToken != ""
}

// IsShort is intentionally conservative. Duration alone cannot distinguish a
// Short from an ordinary short video, so portrait dimensions or a /shorts/ URL
// are required unless yt-dlp explicitly reports media_type=short.
func (v Video) IsShort() bool {
	if v.Short ||
		strings.EqualFold(v.MediaType, "short") ||
		strings.Contains(strings.ToLower(v.WebpageURL), "/shorts/") ||
		strings.Contains(strings.ToLower(v.OriginalURL), "/shorts/") ||
		strings.Contains(strings.ToLower(v.URL), "/shorts/") {
		return true
	}

	return v.Duration > 0 && v.Duration <= 180 && v.Height > v.Width && v.Width > 0
}

// IsPlaylist reports whether the result is a playlist instead of one video.
// HasVideoID reports whether the ID has the form of a YouTube video ID, so it
// is safe to put in a watch page address.
func (v Video) HasVideoID() bool { return validVideoID.MatchString(v.ID) }

// IsChannel reports whether the entry is a channel and not a video. yt-dlp's
// search can return the channel that matches the query as its first result.
func (v Video) IsChannel() bool {
	parsed, err := url.Parse(v.CanonicalURL())
	if err != nil {
		return false
	}
	for _, prefix := range []string{"/channel/", "/@", "/user/", "/c/"} {
		if strings.HasPrefix(parsed.Path, prefix) {
			return true
		}
	}
	return false
}

func (v Video) IsPlaylist() bool {
	return strings.Contains(v.CanonicalURL(), "/playlist?list=")
}

func (v Video) Kind() Kind {
	switch {
	case v.IsPlaylist():
		return KindPlaylist
	case v.IsCurrentlyLive():
		return KindLive
	case v.IsUpcoming():
		return KindUpcoming
	case v.IsPastLive():
		return KindPastLive
	case v.IsShort():
		return KindShort
	default:
		return KindVideo
	}
}

// DurationKnown reports whether DurationText has a real length (or a live or
// upcoming label) to show, and not "duration unknown".
func (v Video) DurationKnown() bool {
	return v.IsCurrentlyLive() || v.IsUpcoming() || v.Duration > 0 || v.DurationString != ""
}

func (v Video) DurationText() string {
	if v.IsCurrentlyLive() {
		if v.ConcurrentViewers > 0 {
			return "LIVE · " + compactCount(v.ConcurrentViewers) + " watching now"
		}
		return "LIVE NOW"
	}
	if v.IsUpcoming() {
		return "UPCOMING"
	}
	// Format the seconds ourselves: yt-dlp's duration_string is a bare "15"
	// for a 15 second Short.
	if v.Duration > 0 {
		return FormatDuration(v.Duration)
	}
	if v.DurationString != "" {
		return v.DurationString
	}
	return "duration unknown"
}

// FormatDuration writes seconds as "m:ss" or "h:mm:ss".
func FormatDuration(duration float64) string {
	seconds := int(duration)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}

	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// PlaylistCountText is the number of videos in a playlist, such as "110
// videos", or "" while it is unknown.
func (v Video) PlaylistCountText() string {
	switch {
	case v.PlaylistCount <= 0:
		return ""
	case v.PlaylistCount == 1:
		return "1 video"
	}
	return fmt.Sprintf("%d videos", v.PlaylistCount)
}

// UpdatedText says when a playlist last changed, such as "updated today", or
// "" while it is unknown.
func (v Video) UpdatedText(now time.Time) string {
	updated, err := time.Parse("20060102", v.ModifiedDate)
	if err != nil {
		return ""
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(updated).Hours() / 24)
	switch {
	case days <= 0:
		return "updated today"
	case days == 1:
		return "updated yesterday"
	case days < 30:
		return fmt.Sprintf("updated %dd ago", days)
	case days < 365:
		return fmt.Sprintf("updated %dmo ago", days/30)
	}
	return fmt.Sprintf("updated %dy ago", days/365)
}

// StartText describes when an upcoming stream begins, e.g. "starts in 2h 10m".
func (v Video) StartText(now time.Time) string {
	if v.ReleaseTimestamp <= 0 {
		return "start time unknown"
	}
	start := time.Unix(int64(v.ReleaseTimestamp), 0)
	delta := start.Sub(now)

	switch {
	case delta <= 0:
		return "starting now"
	case delta < time.Hour:
		return fmt.Sprintf("starts in %dm", int(delta.Minutes())+1)
	case delta < 24*time.Hour:
		return fmt.Sprintf("starts in %dh %dm", int(delta.Hours()), int(delta.Minutes())%60)
	default:
		return "starts " + start.Local().Format("Mon 2 Jan 15:04")
	}
}

func (v Video) ViewsText() string {
	if v.IsCurrentlyLive() && v.ConcurrentViewers > 0 {
		return compactCount(v.ConcurrentViewers) + " watching now"
	}
	if v.ViewCount <= 0 {
		return "views n/a"
	}
	return compactCount(v.ViewCount) + " views"
}

// startedAgo is how long ago a stream started, such as "just now", "25m ago"
// or "3h ago". It counts minutes, because streams are often young.
func startedAgo(delta time.Duration) string {
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	case delta < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(delta.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy ago", int(delta.Hours()/(24*365)))
}

func (v Video) AgeText(now time.Time) string {
	var uploaded time.Time
	switch {
	case v.ReleaseTimestamp > 0:
		uploaded = time.Unix(int64(v.ReleaseTimestamp), 0)
	case v.Timestamp > 0:
		uploaded = time.Unix(int64(v.Timestamp), 0)
	case len(v.UploadDate) == 8:
		if parsed, err := time.Parse("20060102", v.UploadDate); err == nil {
			uploaded = parsed
		}
	}

	if uploaded.IsZero() {
		return "date n/a"
	}

	delta := now.Sub(uploaded)
	if delta < -time.Minute {
		return "scheduled"
	}

	if v.IsCurrentlyLive() {
		// For a stream on air the date is when it started, which is what the
		// reader wants to know, so say it. These are YouTube's own words.
		return "started streaming " + startedAgo(delta)
	}

	if delta < time.Hour {
		return "just now"
	}
	if delta < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	}
	if delta < 30*24*time.Hour {
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	}
	if delta < 365*24*time.Hour {
		return fmt.Sprintf("%dmo ago", int(delta.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy ago", int(delta.Hours()/(24*365)))
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

// MergeDetails overlays richer metadata onto a flat search result without
// replacing useful fields with zero values from partial extractor output.
func MergeDetails(base, details Video) Video {
	base.Detailed = true

	if details.ID != "" {
		base.ID = details.ID
	}
	if details.Title != "" {
		base.Title = details.Title
	}
	if details.WebpageURL != "" {
		base.WebpageURL = details.WebpageURL
	}
	if details.OriginalURL != "" {
		base.OriginalURL = details.OriginalURL
	}
	if details.URL != "" {
		base.URL = details.URL
	}
	if details.Channel != "" {
		base.Channel = details.Channel
	}
	if details.ChannelURL != "" {
		base.ChannelURL = details.ChannelURL
	}
	if details.ChannelID != "" {
		base.ChannelID = details.ChannelID
	}
	if details.ChannelVerified || details.ChannelArtist {
		base.ChannelVerified = true
	}
	if details.ChannelArtist {
		base.ChannelArtist = true
	}
	if details.Uploader != "" {
		base.Uploader = details.Uploader
	}
	if details.Duration > 0 {
		base.Duration = details.Duration
	}
	if details.DurationString != "" {
		base.DurationString = details.DurationString
	}
	if details.ViewCount > 0 {
		base.ViewCount = details.ViewCount
	}
	if details.ConcurrentViewers > 0 {
		base.ConcurrentViewers = details.ConcurrentViewers
	}
	if details.Timestamp > 0 {
		base.Timestamp = details.Timestamp
	}
	if details.ReleaseTimestamp > 0 {
		base.ReleaseTimestamp = details.ReleaseTimestamp
	}
	if details.UploadDate != "" {
		base.UploadDate = details.UploadDate
	}
	if details.Description != "" {
		base.Description = details.Description
	}
	if details.Thumbnail != "" {
		base.Thumbnail = details.Thumbnail
	}
	if details.LiveStatus != "" {
		base.LiveStatus = details.LiveStatus
	}
	if details.IsLive {
		base.IsLive = true
	}
	if details.WasLive {
		base.WasLive = true
	}
	if len(details.Heatmap) > 0 {
		base.Heatmap = details.Heatmap
	}
	if details.StoryboardSpec != "" {
		base.StoryboardSpec = details.StoryboardSpec
	}
	if details.ChatReplayToken != "" {
		base.ChatReplayToken = details.ChatReplayToken
	}
	if details.MediaType != "" {
		base.MediaType = details.MediaType
	}
	if details.Short {
		base.Short = true
	}
	if details.Width > 0 {
		base.Width = details.Width
	}
	if details.Height > 0 {
		base.Height = details.Height
	}
	if details.Availability != "" {
		base.Availability = details.Availability
	}
	if details.PlaylistCount > 0 {
		base.PlaylistCount = details.PlaylistCount
	}
	if details.ModifiedDate != "" {
		base.ModifiedDate = details.ModifiedDate
	}
	if len(details.Entries) > 0 {
		base.Entries = details.Entries
	}

	return base
}
