package media

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)

// Video contains the subset of yt-dlp metadata used by ytwatch.
type Video struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	WebpageURL        string  `json:"webpage_url"`
	OriginalURL       string  `json:"original_url"`
	URL               string  `json:"url"`
	Channel           string  `json:"channel"`
	ChannelURL        string  `json:"channel_url"`
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
}

type Kind int

const (
	KindVideo Kind = iota
	KindLive
	KindUpcoming
	KindPastLive
	KindShort
)

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

func (v Video) ThumbnailURL() string {
	parsed, err := url.Parse(v.Thumbnail)
	if err == nil && parsed.Scheme == "https" && parsed.Host != "" {
		return v.Thumbnail
	}
	if validVideoID.MatchString(v.ID) {
		return "https://i.ytimg.com/vi/" + v.ID + "/mqdefault.jpg"
	}
	return ""
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

// IsShort is intentionally conservative. Duration alone cannot distinguish a
// Short from an ordinary short video, so portrait dimensions or a /shorts/ URL
// are required unless yt-dlp explicitly reports media_type=short.
func (v Video) IsShort() bool {
	if v.Short || strings.EqualFold(v.MediaType, "short") || strings.Contains(strings.ToLower(v.WebpageURL), "/shorts/") || strings.Contains(strings.ToLower(v.OriginalURL), "/shorts/") || strings.Contains(strings.ToLower(v.URL), "/shorts/") {
		return true
	}
	return v.Duration > 0 && v.Duration <= 180 && v.Height > v.Width && v.Width > 0
}

func (v Video) Kind() Kind {
	switch {
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

// Badge returns a small status label; ordinary videos are deliberately not
// prefixed with a noisy VIDEO label.
func (v Video) Badge() string {
	switch v.Kind() {
	case KindLive:
		return "🔴 LIVE"
	case KindUpcoming:
		return "⏳ UPCOMING"
	case KindPastLive:
		return "📼 LIVE VOD"
	case KindShort:
		return "⚡ SHORT"
	default:
		return ""
	}
}

func (v Video) DurationText() string {
	if v.IsCurrentlyLive() {
		if v.ConcurrentViewers > 0 {
			return "LIVE · " + compactCount(v.ConcurrentViewers) + " watching"
		}
		return "LIVE NOW"
	}
	if v.IsUpcoming() {
		return "UPCOMING"
	}
	if v.DurationString != "" {
		return v.DurationString
	}
	if v.Duration <= 0 {
		return "duration unknown"
	}
	seconds := int(v.Duration)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
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
		return compactCount(v.ConcurrentViewers) + " watching"
	}
	if v.ViewCount <= 0 {
		return "views n/a"
	}
	return compactCount(v.ViewCount) + " views"
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
	return base
}
