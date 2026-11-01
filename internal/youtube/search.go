package youtube

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

const (
	EventResult     = "result"
	EventDetails    = "details"
	EventSearchDone = "search-done"
	EventDetailsEnd = "details-done"

	// ResultLimit is how many results a search shows, whatever the filters and
	// [search] hide leave out. The sources together return more; the list keeps
	// the best ranked ones.
	ResultLimit = 50

	videoResultCount = ResultLimit
	liveResultCount  = 20
	playlistCount    = 10
	shortResultCount = 6

	// A filtered search is one query on the results page.
	filteredResultCount   = ResultLimit
	filteredPlaylistCount = ResultLimit
)

type Event struct {
	Kind  string
	Video media.Video
	Count int
	Err   error
	// Rank orders results: a lower rank is shown higher. Queries finish in any
	// order, so the interface places each result by its rank instead of by
	// arrival, which keeps YouTube's own ordering.
	Rank int
}

// ranking turns the position of a result in its query into a Event.Rank:
// base + position*step. Queries use different bases so their results mix the
// way a reader expects: the main query first, a playlist every few results,
// and extra live streams after them.
type ranking struct {
	base, step int
	// onlyLive keeps only streams that are on air or upcoming. The query that
	// looks for live streams returns unrelated videos otherwise.
	onlyLive bool
}

func (r ranking) at(position int) int { return r.base + position*r.step }

// The main query's results are 10 apart (0, 10, 20 ...), so a playlist lands
// after the 8th result, then after every 4th. The other sources use ranks
// that end in a different digit, so two results never have the same rank and
// the order does not depend on which query finishes first.
var (
	mainRanking     = ranking{base: 0, step: 10}
	playlistRanking = ranking{base: 86, step: 40}
	liveRanking     = ranking{base: 365, step: 10, onlyLive: true}
)

var (
	errNoVideos       = errors.New("no videos found in this playlist")
	errBrowseTimedOut = errors.New("loading timed out; showing the videos found so far")
)

// fetchCount is how many results the main query asks for. Streams and
// recordings that are hidden are dropped from it, so it asks for half as many
// again, to still have ResultLimit results to show.
func fetchCount(count int, hidden map[media.Kind]bool) int {
	if hidden[media.KindLive] || hidden[media.KindUpcoming] || hidden[media.KindPastLive] {
		return count * 3 / 2
	}
	return count
}

// hiddenKinds is the set of kinds a search leaves out: those in [search] hide,
// except the kinds that the filter asks for, so a Shorts search still finds
// Shorts when they are hidden. The Videos filter also leaves out streams: on
// YouTube it includes the ones on air, but the Live filter is for those.
func hiddenKinds(hide []media.Kind, f Filters) map[media.Kind]bool {
	if len(hide) == 0 && f.Type != TypeVideos {
		return nil
	}
	hidden := map[media.Kind]bool{}
	for _, kind := range hide {
		hidden[kind] = true
	}
	switch f.Type {
	case TypeVideos:
		hidden[media.KindLive] = true
		hidden[media.KindUpcoming] = true
	case TypeShorts:
		delete(hidden, media.KindShort)
	case TypeLive:
		delete(hidden, media.KindLive)
		delete(hidden, media.KindUpcoming)
	case TypePlaylists:
		delete(hidden, media.KindPlaylist)
	}
	return hidden
}

// channelStreamBoost moves the streams that are on air now from the channel the
// user searched for above every other result. It keeps the order within the
// group, and ranks of different sources still never tie.
const channelStreamBoost = 1_000_000

// channelLookahead is how many results the main query reads before it gives up
// on finding a channel among them.
const channelLookahead = 5

// topChannel is the channel that YouTube matched to the query, found by the
// main query. The live query waits for it, because a stream is promoted only
// when it belongs to that channel.
type topChannel struct {
	once    sync.Once
	ready   chan struct{}
	id      string // written once before ready is closed
	query   string // the search text, folded by foldName
	pending atomic.Int32
}

// newTopChannel makes a topChannel that is settled by the first channel found
// or, when none is, after sources calls of finish.
func newTopChannel(query string, sources int) *topChannel {
	c := &topChannel{ready: make(chan struct{}), query: foldName(query)}
	c.pending.Store(int32(sources))
	return c
}

// finish says that one source has given up looking for the channel.
func (c *topChannel) finish() {
	if c.pending.Add(-1) <= 0 {
		c.settle("")
	}
}

// matches reports whether a stream belongs to the channel the user searched
// for. With a channel ID that is exact. Without one (YouTube does not always
// return the channel among the results) the channel name is compared with the
// query instead: typing the name of a channel is the usual way to look for it.
func (c *topChannel) matches(v media.Video, id string) bool {
	if id != "" {
		return v.ChannelID == id
	}
	name := foldName(v.Channel)
	return len([]rune(name)) >= minChannelNameLength && strings.Contains(c.query, name)
}

// minChannelNameLength keeps very short channel names, which are common words
// inside other queries, from matching by name.
const minChannelNameLength = 4

// nameFolder removes accents, so "CazéTV" and "cazetv" compare equal.
var nameFolder = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// foldName lowercases s, removes accents and keeps only letters and digits.
func foldName(s string) string {
	plain, _, err := transform.String(nameFolder, s)
	if err != nil {
		plain = s
	}
	var b strings.Builder
	for _, r := range strings.ToLower(plain) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// settle fixes the channel ID, or "" for no channel. Only the first call counts.
func (c *topChannel) settle(id string) {
	c.once.Do(func() {
		c.id = id
		close(c.ready)
	})
}

// current is the channel ID if it is known already, otherwise "".
func (c *topChannel) current() string {
	select {
	case <-c.ready:
		return c.id
	default:
		return ""
	}
}

// wait blocks until the channel is known or the search ends.
func (c *topChannel) wait(ctx context.Context) string {
	select {
	case <-c.ready:
		return c.id
	case <-ctx.Done():
		return ""
	}
}

type queryResult struct {
	rows int
	err  error
}

// Search runs a normal relevance search alongside a live-oriented search and a
// playlist search. With filters it runs one query on the results page instead,
// because only that page supports them. All of them stream flat results
// immediately. The search itself does no detail lookups, which are the main
// CPU cost; the interface asks for the details of the rows near the selection
// with FetchDetailsBatch.
func Search(
	ctx context.Context,
	query string,
	filters Filters,
	hide []media.Kind,
	loc locale.Locale,
	emit func(Event),
) {
	hidden := hiddenKinds(hide, filters)
	query = strings.TrimSpace(query)

	if query == "" {
		emit(Event{Kind: EventSearchDone, Err: errors.New("enter a search query")})
		emit(Event{Kind: EventDetailsEnd})
		return
	}

	searchCtx, cancelSearch := context.WithTimeout(ctx, 25*time.Second)
	defer cancelSearch()

	var seen sync.Map
	var uniqueCount atomic.Int64
	var searchWG sync.WaitGroup

	// The live page has nothing to show when live and upcoming are hidden.
	skipLive := hidden[media.KindLive] && hidden[media.KindUpcoming]

	// The main query and the live page can each return the matched channel.
	sources := 2
	if skipLive {
		sources = 1
	}
	channel := newTopChannel(query, sources)

	var queries []func() queryResult
	if filters.Active() {
		if filters.Type == TypeShorts {
			queries = append(queries, func() queryResult {
				return runShortsQuery(
					searchCtx,
					query,
					filters,
					loc,
					0,
					func(position int, _ string) int { return mainRanking.at(position) },
					&seen,
					&uniqueCount,
					emit,
				)
			})
		} else if filters.Type == TypePlaylists {
			queries = append(queries, func() queryResult {
				return runPlaylistQuery(
					searchCtx,
					resultsURL(query, filters),
					filteredPlaylistCount,
					loc,
					mainRanking,
					&seen,
					&uniqueCount,
					emit,
				)
			})
		} else {
			queries = append(queries, func() queryResult {
				return runQuery(
					searchCtx,
					resultsURL(query, filters),
					fetchCount(filteredResultCount, hidden),
					loc,
					mainRanking,
					filters.Type == TypeLive,
					nil,
					hidden,
					&seen,
					&uniqueCount,
					emit,
				)
			})
		}
	} else {
		queries = append(queries, func() queryResult {
			return runQuery(
				searchCtx,
				fmt.Sprintf("ytsearch%d:%s", fetchCount(videoResultCount, hidden), query),
				0,
				loc,
				mainRanking,
				false,
				channel,
				hidden,
				&seen,
				&uniqueCount,
				emit,
			)
		})
		if !skipLive {
			queries = append(queries, func() queryResult {
				// The results page filtered to live streams, in YouTube's own
				// order. The normal search misses streams that YouTube shows
				// in a "live now" card, and has no viewer-based order.
				return runQuery(
					searchCtx,
					resultsURL(query, Filters{Type: TypeLive}),
					liveResultCount,
					loc,
					liveRanking,
					true,
					channel,
					hidden,
					&seen,
					&uniqueCount,
					emit,
				)
			})
		}
		// Hidden kinds are not even requested.
		if !hidden[media.KindPlaylist] {
			queries = append(queries, func() queryResult {
				return runPlaylistQuery(
					searchCtx,
					playlistSearchURL(query),
					playlistCount,
					loc,
					playlistRanking,
					&seen,
					&uniqueCount,
					emit,
				)
			})
		}
		if !hidden[media.KindShort] {
			queries = append(queries, func() queryResult {
				// yt-dlp's search never returns Shorts, so they come from the
				// Shorts results page.
				return runShortsQuery(
					searchCtx,
					query,
					Filters{Type: TypeShorts},
					loc,
					shortResultCount,
					shortRanking(query),
					&seen,
					&uniqueCount,
					emit,
				)
			})
		}
	}

	queryResults := make(chan queryResult, len(queries))
	for _, run := range queries {
		searchWG.Add(1)
		go func() {
			defer searchWG.Done()
			queryResults <- run()
		}()
	}

	searchWG.Wait()

	results := make([]queryResult, len(queries))
	for i := range results {
		results[i] = <-queryResults
	}

	var searchErr error
	if uniqueCount.Load() == 0 {
		var messages []string
		for _, result := range results {
			if result.err != nil {
				messages = append(messages, result.err.Error())
			}
		}
		// No rows and no failed query is an empty result, not an error. The
		// interface explains it, including which filters are on.
		if len(messages) > 0 {
			searchErr = errors.New(strings.Join(messages, "; "))
		}
	} else if searchCtx.Err() != nil && ctx.Err() == nil {
		// Partial results are useful even if a slow search times out.
		searchErr = fmt.Errorf("search timed out; showing partial results")
	}
	emit(Event{Kind: EventSearchDone, Count: int(uniqueCount.Load()), Err: searchErr})

	// The search does not look up details; the interface asks for those of the
	// rows near the selection (see FetchDetailsBatch).
	emit(Event{Kind: EventDetailsEnd, Count: int(uniqueCount.Load())})
}

// runQuery runs one yt-dlp search and streams its results. source is a
// "ytsearchN:query" spec or a results-page URL; listEnd limits how many
// entries a URL returns (a ytsearch spec carries its own count, so it is 0).
func runQuery(
	ctx context.Context,
	source string,
	listEnd int,
	loc locale.Locale,
	order ranking,
	liveQuery bool,
	channel *topChannel,
	hidden map[media.Kind]bool,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	emit func(Event),
) queryResult {
	// A query that is done looking for the channel tells so, once; the
	// channel is "none" only when all of them are done.
	channelChecked := false
	checkedChannel := func() {
		if channel != nil && !channelChecked {
			channelChecked = true
			channel.finish()
		}
	}
	defer checkedChannel()

	args := []string{
		"--flat-playlist", "--dump-json", "--no-warnings", "--ignore-errors",
		"--skip-download", "--socket-timeout", "7",
		"--extractor-retries", "1", "--retries", "1",
		"--extractor-args", "youtubetab:approximate_date",
	}
	if listEnd > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(listEnd))
	}
	args = append(args, loc.YtDlpArgs()...)
	args = append(args, source)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return queryResult{err: err}
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return queryResult{err: fmt.Errorf("start yt-dlp: %w", err)}
	}
	lowPriority(cmd)

	rows := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		var video media.Video
		if err := json.Unmarshal(scanner.Bytes(), &video); err != nil || video.ID == "" {
			continue
		}
		if video.IsChannel() {
			// A channel is not a video and has no page here. Its ID tells which
			// streams belong to the channel the user searched for.
			if channel != nil {
				channel.settle(video.ID)
			}
			continue
		}
		position := rows
		rows++
		if rows >= channelLookahead {
			checkedChannel() // no channel among the first results
		}

		if unavailable(video.Title) {
			continue
		}
		if order.onlyLive && !video.IsCurrentlyLive() && !video.IsUpcoming() {
			continue
		}
		if hidden[video.Kind()] {
			continue
		}

		if video.Title == "" {
			video.Title = "Untitled video"
		}
		if video.WebpageURL == "" && video.OriginalURL == "" && video.URL == "" {
			video.WebpageURL = video.CanonicalURL()
		}

		_, duplicate := seen.LoadOrStore(video.ID, struct{}{})
		if !duplicate {
			count := int(uniqueCount.Add(1))
			rank := order.at(position)
			if channel != nil && video.IsCurrentlyLive() {
				id := channel.current()
				if liveQuery {
					checkedChannel() // do not wait for ourselves
					id = channel.wait(ctx)
				}
				if channel.matches(video, id) {
					rank -= channelStreamBoost
				}
			}
			emit(Event{Kind: EventResult, Video: video, Count: count, Rank: rank})
		}
	}

	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if scanErr != nil {
		return queryResult{rows: rows, err: scanErr}
	}
	if rows == 0 && waitErr != nil {
		message := lastUsefulLine(stderr.String())
		if message == "" {
			message = waitErr.Error()
		}
		return queryResult{err: errors.New(message)}
	}

	return queryResult{rows: rows}
}

// playlistSearchURL is YouTube's results page filtered to playlists. The ytsearch
// prefix cannot return playlists.
func playlistSearchURL(query string) string {
	return resultsURL(query, Filters{Type: TypePlaylists})
}

// runPlaylistQuery emits playlist results. Their size and first videos come
// later, from a detail lookup of the selected playlist.
func runPlaylistQuery(
	ctx context.Context,
	source string,
	limit int,
	loc locale.Locale,
	order ranking,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	emit func(Event),
) queryResult {
	args := []string{
		"--flat-playlist", "--dump-json", "--no-warnings", "--ignore-errors",
		"--skip-download", "--socket-timeout", "7",
		"--extractor-retries", "1", "--retries", "1",
		"--playlist-end", strconv.Itoa(limit),
	}
	args = append(args, loc.YtDlpArgs()...)

	cmd := exec.CommandContext(ctx, "yt-dlp", append(args, source)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return queryResult{err: err}
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return queryResult{err: fmt.Errorf("start yt-dlp: %w", err)}
	}
	lowPriority(cmd)

	rows := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		var playlist media.Video
		if err := json.Unmarshal(scanner.Bytes(), &playlist); err != nil ||
			playlist.ID == "" ||
			!playlist.IsPlaylist() {
			continue
		}

		position := rows
		rows++

		if _, duplicate := seen.LoadOrStore(playlist.ID, struct{}{}); !duplicate {
			emit(Event{
				Kind:  EventResult,
				Video: playlist,
				Count: int(uniqueCount.Add(1)),
				Rank:  order.at(position),
			})
		}
	}

	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if scanErr != nil {
		return queryResult{rows: rows, err: scanErr}
	}
	if rows == 0 && waitErr != nil {
		message := lastUsefulLine(stderr.String())
		if message == "" {
			message = waitErr.Error()
		}
		return queryResult{err: errors.New(message)}
	}

	return queryResult{rows: rows}
}

// playlistPreviewVideos is how many videos a playlist lookup lists.
const playlistPreviewVideos = 3

// FetchDetailsBatch looks up the details of videos and calls found for each
// one as soon as it arrives. Videos are read from their watch pages, a few at a
// time, which is quick and uses almost no CPU. Playlists, and videos whose page
// cannot be read, go to yt-dlp, with as few runs as possible. It returns an
// error only when yt-dlp was needed and could not run or gave nothing.
func FetchDetailsBatch(
	parent context.Context,
	videos []media.Video,
	loc locale.Locale,
	found func(media.Video),
) error {
	var singles, playlists []media.Video

	for _, v := range videos {
		switch {
		case v.CanonicalURL() == "":
		case v.IsPlaylist():
			playlists = append(playlists, v)
		default:
			singles = append(singles, v)
		}
	}

	// Videos with an ID that does not look like a video ID are not put in a
	// page address; yt-dlp handles them.
	var readable, unreadable []media.Video
	for _, v := range singles {
		if v.HasVideoID() {
			readable = append(readable, v)
		} else {
			unreadable = append(unreadable, v)
		}
	}
	singles = append(unreadable, fetchWatchDetails(parent, readable, loc, found)...)

	var firstErr error
	for _, group := range [][]media.Video{singles, playlists} {
		if len(group) == 0 {
			continue
		}
		if err := fetchGroup(parent, group, loc, found); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// fetchGroup runs yt-dlp once for videos that are all playlists or all not.
func fetchGroup(
	parent context.Context,
	group []media.Video,
	loc locale.Locale,
	found func(media.Video),
) error {
	ctx, cancel := context.WithTimeout(parent, 9*time.Second+time.Duration(len(group)-1)*6*time.Second)
	defer cancel()

	playlist := group[0].IsPlaylist()
	args := []string{
		"--dump-single-json", "--no-warnings", "--no-progress", "--skip-download",
		"--no-playlist", "--ignore-no-formats-error", "--ignore-errors",
		"--socket-timeout", "6", "--extractor-retries", "1", "--retries", "1",
	}
	if playlist {
		// A flat lookup of the first videos is quick and still reports the
		// playlist's size, update date and description.
		args = []string{
			"--flat-playlist", "--dump-single-json", "--no-warnings", "--no-progress",
			"--ignore-errors",
			"--playlist-end", strconv.Itoa(playlistPreviewVideos),
			"--socket-timeout", "6", "--extractor-retries", "1", "--retries", "1",
		}
	}
	// Only metadata is wanted, not streams, so the player scripts are skipped.
	// The fields used are the same without them, and each lookup takes about
	// half the time and CPU. A playlist lookup does not use the player.
	var youtubeArgs []string
	if !playlist {
		youtubeArgs = []string{"player_skip=js,configs", "player_client=web"}
	}
	args = append(loc.YtDlpArgs(youtubeArgs...), args...)

	byID := make(map[string]media.Video, len(group))
	for _, v := range group {
		byID[v.ID] = v
		args = append(args, v.CanonicalURL())
	}

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}
	lowPriority(cmd)

	got := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var details media.Video
		if err := json.Unmarshal(scanner.Bytes(), &details); err != nil {
			continue
		}
		base, ok := byID[details.ID]
		if !ok {
			continue
		}
		got++
		found(media.MergeDetails(base, details))
	}
	waitErr := cmd.Wait()

	if got == 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if message := lastUsefulLine(stderr.String()); message != "" {
			return errors.New(message)
		}
		return waitErr
	}
	return nil
}

// lowPriority makes the yt-dlp process nicer to the rest of the system, so the
// interface and other programs stay smooth during a search.
func lowPriority(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	}
}

func lastUsefulLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
