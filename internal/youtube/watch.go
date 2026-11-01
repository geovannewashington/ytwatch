package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// Details of a video are read from its watch page, which holds them as JSON in
// "var ytInitialPlayerResponse" and "var ytInitialData". That takes about a
// second and almost no CPU, while a yt-dlp run costs seconds of CPU. When the
// page cannot be read, the caller falls back to yt-dlp.
const (
	playerResponseMarker = "var ytInitialPlayerResponse = "

	// watchWorkers is how many watch pages are downloaded at the same time.
	watchWorkers = 5

	// watchTimeout limits one watch page, so a slow one does not hold the rest.
	watchTimeout = 12 * time.Second
)

// playerResponse is the part of ytInitialPlayerResponse that ytwatch uses.
// Unknown fields are skipped by the JSON decoder.
type playerResponse struct {
	VideoDetails struct {
		VideoID          string `json:"videoId"`
		Title            string `json:"title"`
		LengthSeconds    string `json:"lengthSeconds"`
		ChannelID        string `json:"channelId"`
		Author           string `json:"author"`
		ShortDescription string `json:"shortDescription"`
		ViewCount        string `json:"viewCount"`
		IsLive           bool   `json:"isLive"`
		IsUpcoming       bool   `json:"isUpcoming"`
		IsLiveContent    bool   `json:"isLiveContent"`
	} `json:"videoDetails"`

	Microformat struct {
		Renderer struct {
			PublishDate          string `json:"publishDate"`
			LiveBroadcastDetails struct {
				StartTimestamp string `json:"startTimestamp"`
			} `json:"liveBroadcastDetails"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`

	// The size of the video tells a Short (tall) from other videos.
	StreamingData struct {
		Formats         []formatSize `json:"formats"`
		AdaptiveFormats []formatSize `json:"adaptiveFormats"`
	} `json:"streamingData"`
}

type formatSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// watchData is the part of ytInitialData that holds the number of people
// watching a stream.
type watchData struct {
	Contents struct {
		TwoColumn struct {
			Results struct {
				Results struct {
					Contents []struct {
						PrimaryInfo struct {
							ViewCount struct {
								Renderer struct {
									IsLive            bool   `json:"isLive"`
									OriginalViewCount string `json:"originalViewCount"`
								} `json:"videoViewCountRenderer"`
							} `json:"viewCount"`
						} `json:"videoPrimaryInfoRenderer"`
					} `json:"contents"`
				} `json:"results"`
			} `json:"results"`
		} `json:"twoColumnWatchNextResults"`
	} `json:"contents"`
}

// viewers is the number of people watching now, or 0 when the page has none.
func (d watchData) viewers() int64 {
	for _, item := range d.Contents.TwoColumn.Results.Results.Contents {
		renderer := item.PrimaryInfo.ViewCount.Renderer
		if !renderer.IsLive {
			continue
		}

		if count, err := strconv.ParseInt(renderer.OriginalViewCount, 10, 64); err == nil {
			return count
		}
	}

	return 0
}

func watchURL(id string, loc locale.Locale) string {
	u := "https://www.youtube.com/watch?v=" + url.QueryEscape(id)
	if params := loc.PageParams(); params != "" {
		u += "&" + params
	}

	return u
}

// decodeAfter decodes the JSON value that follows marker in page. ok is false
// when the marker is missing.
func decodeAfter(page []byte, marker string, into any) (ok bool, err error) {
	start := bytes.Index(page, []byte(marker))
	if start < 0 {
		return false, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(page[start+len(marker):]))
	if err := decoder.Decode(into); err != nil {
		return true, fmt.Errorf("read watch page: %w", err)
	}

	return true, nil
}

// parseWatchPage reads the details of the video id from its watch page. It
// returns an error when the page is not a watch page for that video, for
// example a consent or sign-in page; the caller then asks yt-dlp instead.
func parseWatchPage(page []byte, id string) (media.Video, error) {
	var player playerResponse
	found, err := decodeAfter(page, playerResponseMarker, &player)
	if err != nil {
		return media.Video{}, err
	}

	if !found {
		return media.Video{}, errors.New("unexpected watch page")
	}

	details := player.VideoDetails
	if details.VideoID != id || details.Title == "" {
		return media.Video{}, errors.New("watch page without video details")
	}

	// The number of viewers is optional: a page without it still has details.
	var data watchData
	_, _ = decodeAfter(page, initialDataMarker, &data)

	video := media.Video{
		ID:          details.VideoID,
		Title:       details.Title,
		WebpageURL:  "https://www.youtube.com/watch?v=" + details.VideoID,
		Channel:     details.Author,
		Uploader:    details.Author,
		Description: details.ShortDescription,
		LiveStatus:  liveStatus(details.IsLive, details.IsUpcoming, details.IsLiveContent),
		IsLive:      details.IsLive,
		WasLive:     details.IsLiveContent && !details.IsLive && !details.IsUpcoming,
	}

	if details.ChannelID != "" {
		video.ChannelID = details.ChannelID
		video.ChannelURL = "https://www.youtube.com/channel/" + details.ChannelID
	}

	if seconds, err := strconv.ParseFloat(details.LengthSeconds, 64); err == nil && seconds > 0 {
		video.Duration = seconds
	}

	if views, err := strconv.ParseInt(details.ViewCount, 10, 64); err == nil {
		video.ViewCount = views
	}

	if details.IsLive {
		video.ConcurrentViewers = data.viewers()
	}

	published := player.Microformat.Renderer.PublishDate
	if when, err := time.Parse(time.RFC3339, published); err == nil {
		video.Timestamp = float64(when.Unix())
	}

	if len(published) >= len("2006-01-02") {
		video.UploadDate = strings.ReplaceAll(published[:len("2006-01-02")], "-", "")
	}

	start := player.Microformat.Renderer.LiveBroadcastDetails.StartTimestamp
	if when, err := time.Parse(time.RFC3339, start); err == nil {
		video.ReleaseTimestamp = float64(when.Unix())
	}

	video.Width, video.Height = firstSize(player)

	return video, nil
}

// liveStatus gives the same names as yt-dlp's live_status.
func liveStatus(live, upcoming, wasLive bool) string {
	switch {
	case live:
		return "is_live"
	case upcoming:
		return "is_upcoming"
	case wasLive:
		return "was_live"
	}

	return "not_live"
}

// firstSize is the size of the first video format of the page, or 0, 0.
func firstSize(player playerResponse) (width, height int) {
	for _, formats := range [][]formatSize{
		player.StreamingData.Formats,
		player.StreamingData.AdaptiveFormats,
	} {
		for _, format := range formats {
			if format.Width > 0 && format.Height > 0 {
				return format.Width, format.Height
			}
		}
	}

	return 0, 0
}

// fetchWatchDetails downloads the watch pages of videos, a few at a time, and
// calls found with each video merged with its details as soon as it is ready.
// found is never called from two goroutines at once. It returns the videos
// whose page could not be read, so the caller can ask yt-dlp about them. When
// ctx ends it stops and returns nothing, because those videos are not wanted
// any more.
func fetchWatchDetails(
	ctx context.Context,
	videos []media.Video,
	loc locale.Locale,
	found func(media.Video),
) []media.Video {
	var (
		mu     sync.Mutex
		failed []media.Video
		wg     sync.WaitGroup
		slots  = make(chan struct{}, watchWorkers)
	)

start:
	for _, v := range videos {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break start
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			details, err := fetchWatchDetail(ctx, v, loc)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failed = append(failed, v)
				return
			}
			found(media.MergeDetails(v, details))
		}()
	}

	wg.Wait()

	if ctx.Err() != nil {
		return nil
	}

	return failed
}

func fetchWatchDetail(ctx context.Context, v media.Video, loc locale.Locale) (media.Video, error) {
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()

	page, err := fetchResultsPage(ctx, watchURL(v.ID, loc), loc)
	if err != nil {
		return media.Video{}, err
	}

	return parseWatchPage(page, v.ID)
}
