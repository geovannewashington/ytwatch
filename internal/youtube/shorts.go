package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

// yt-dlp reads only a few entries of the Shorts shelf on the results page, so
// Shorts are read from the page directly. The page holds its data as JSON in
// "var ytInitialData".
const initialDataMarker = "var ytInitialData = "

// shortsURL is the results page for a Shorts search. The parameter is escaped
// once here, because it is not passed through yt-dlp.
func shortsURL(query string, f Filters, loc locale.Locale) string {
	u := "https://www.youtube.com/results?search_query=" + url.QueryEscape(query)
	if param := f.Param(); param != "" {
		u += "&sp=" + url.QueryEscape(param)
	}
	if params := loc.PageParams(); params != "" {
		u += "&" + params
	}
	return u
}

// parseShorts returns the Shorts listed on a results page, in page order.
func parseShorts(page []byte) ([]media.Video, error) {
	start := bytes.Index(page, []byte(initialDataMarker))
	if start < 0 {
		return nil, errors.New("unexpected results page")
	}
	var data any
	decoder := json.NewDecoder(bytes.NewReader(page[start+len(initialDataMarker):]))
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("read results page: %w", err)
	}
	type entry struct {
		index float64
		video media.Video
	}
	var found []entry
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if lockup, ok := n["shortsLockupViewModel"].(map[string]any); ok {
				if v, ok := shortFromLockup(lockup); ok {
					index, _ := lockup["indexInCollection"].(float64)
					found = append(found, entry{index, v})
				}
			}
			for _, child := range n {
				walk(child)
			}
		case []any:
			for _, child := range n {
				walk(child)
			}
		}
	}
	walk(data)
	// Decoded maps have no order, so restore the order of the page.
	sort.SliceStable(found, func(i, j int) bool { return found[i].index < found[j].index })
	var videos []media.Video
	seen := map[string]bool{}
	for _, e := range found {
		if !seen[e.video.ID] {
			seen[e.video.ID] = true
			videos = append(videos, e.video)
		}
	}
	return videos, nil
}

func shortFromLockup(lockup map[string]any) (media.Video, bool) {
	id := strings.TrimPrefix(str(lockup["entityId"]), "shorts-shelf-item-")
	if id == "" {
		return media.Video{}, false
	}
	title := text(lockup, "overlayMetadata", "primaryText", "content")
	if title == "" {
		// "Title, 16 million views - play Short"
		title = str(lockup["accessibilityText"])
		if i := strings.LastIndex(title, ", "); i > 0 {
			title = title[:i]
		}
	}
	if title == "" {
		title = "Untitled Short"
	}
	return media.Video{
		ID:         id,
		Title:      title,
		WebpageURL: "https://www.youtube.com/shorts/" + id,
		Short:      true,
	}, true
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// text follows a path of object keys and returns the string at its end.
func text(node map[string]any, path ...string) string {
	var current any = node
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	return str(current)
}

// Ranks of Shorts in a mixed search, chosen so that they never equal the rank
// of another source (see mainRanking).
const (
	shortMatchOffset = 3  // after the regular result at the same position
	shortFirstRank   = 34 // YouTube's best Short for the query, after 4 results
	shortGroupBase   = 52 // the other Shorts, after the first few results
)

// shortRanking places Shorts in a mixed search. A Short with exactly the title
// searched for goes first. A Short whose title has every
// word of the query is as good a match as a regular result, so it goes right
// after the regular result at the same position. The first Short of the page
// is YouTube's best match, even when its title is translated and so does not
// have the words searched for, so it comes next. The other Shorts form a group
// after the first few results, like the Shorts shelf on youtube.com.
func shortRanking(query string) func(position int, title string) int {
	words := queryWords(query)
	exact := strings.Join(words, " ")
	return func(position int, title string) int {
		if exact != "" && strings.Join(queryWords(title), " ") == exact {
			return -1
		}
		if len(words) > 0 && containsAll(title, words) {
			return mainRanking.at(position) + shortMatchOffset
		}
		if position == 0 {
			return shortFirstRank
		}
		return shortGroupBase + position*3
	}
}

// queryWords splits text into lower-case words, ignoring punctuation.
func queryWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// containsAll reports whether title has every word.
func containsAll(title string, words []string) bool {
	lower := strings.Join(queryWords(title), " ")
	for _, w := range words {
		if !strings.Contains(lower, w) {
			return false
		}
	}
	return true
}

// fetchResultsPage downloads a results page. It is a variable so that tests do
// not use the network.
var fetchResultsPage = func(ctx context.Context, pageURL string, loc locale.Locale) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	request.Header.Set("Accept-Language", loc.AcceptLanguage())
	// Skips the cookie consent page that some regions get.
	request.Header.Set("Cookie", "SOCS=CAI")
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube returned %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
}

// runShortsQuery fetches the Shorts of a results page and queues each one for
// a detail lookup, which adds the length, channel and age. limit caps how many
// are used (0 means all), and rank gives each one its place in the list.
func runShortsQuery(
	ctx context.Context,
	query string,
	f Filters,
	loc locale.Locale,
	limit int,
	rank func(position int, title string) int,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	jobs chan<- media.Video,
	emit func(Event),
) queryResult {
	page, err := fetchResultsPage(ctx, shortsURL(query, f, loc), loc)
	if err != nil {
		return queryResult{err: err}
	}
	shorts, err := parseShorts(page)
	if err != nil {
		return queryResult{err: err}
	}
	if limit > 0 && len(shorts) > limit {
		shorts = shorts[:limit]
	}
	for position, short := range shorts {
		if ctx.Err() != nil {
			break
		}
		if _, duplicate := seen.LoadOrStore(short.ID, struct{}{}); duplicate {
			continue
		}
		emit(Event{Kind: EventResult, Video: short, Count: int(uniqueCount.Add(1)), Rank: rank(position, short.Title)})
		select {
		case jobs <- short:
		case <-ctx.Done():
		}
	}
	return queryResult{rows: len(shorts)}
}
