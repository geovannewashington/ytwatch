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

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
)

const (
	EventResult     = "result"
	EventDetails    = "details"
	EventSearchDone = "search-done"
	EventDetailsEnd = "details-done"

	videoResultCount = 35
	liveResultCount  = 20
	playlistCount    = 10
	shortResultCount = 6

	// A filtered search is one query on the results page.
	filteredResultCount   = 50
	filteredPlaylistCount = 30

	// Detail lookups are the main CPU cost of a search: every yt-dlp run
	// starts Python and loads all extractors. So they are batched, run by few
	// workers at a low priority, and done up front only for the first rows;
	// the interface asks for the rest when a row is selected.
	metadataWorkers = 2
	detailBatchSize = 5
	detailBatchWait = 150 * time.Millisecond
	eagerVideos     = 20
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

type queryResult struct {
	rows int
	err  error
}

// Search runs a normal relevance search alongside a live-oriented search and a
// playlist search. With filters it runs one query on the results page instead,
// because only that page supports them. All of them stream flat results
// immediately; richer metadata is resolved by a bounded worker pool so stream
// status and Shorts details can update later.
func Search(ctx context.Context, query string, filters Filters, loc locale.Locale, emit func(Event)) {
	query = strings.TrimSpace(query)
	if query == "" {
		emit(Event{Kind: EventSearchDone, Err: errors.New("enter a search query")})
		emit(Event{Kind: EventDetailsEnd})
		return
	}

	searchCtx, cancelSearch := context.WithTimeout(ctx, 25*time.Second)
	defer cancelSearch()

	regularJobs := make(chan media.Video, videoResultCount)
	liveJobs := make(chan media.Video, liveResultCount)
	workers := startDetailWorkers(ctx, regularJobs, liveJobs, loc, emit)

	var seen sync.Map
	var uniqueCount atomic.Int64
	var searchWG sync.WaitGroup
	var queries []func() queryResult
	if filters.Active() {
		if filters.Type == TypeShorts {
			queries = append(queries, func() queryResult {
				return runShortsQuery(searchCtx, query, filters, loc, 0, func(position int, _ string) int { return mainRanking.at(position) }, &seen, &uniqueCount, regularJobs, emit)
			})
		} else if filters.Type == TypePlaylists {
			queries = append(queries, func() queryResult {
				return runPlaylistQuery(searchCtx, resultsURL(query, filters), filteredPlaylistCount, loc, mainRanking, &seen, &uniqueCount, liveJobs, emit)
			})
		} else {
			queries = append(queries, func() queryResult {
				return runQuery(searchCtx, resultsURL(query, filters), filteredResultCount, loc, mainRanking, filters.Type == TypeLive, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			})
		}
	} else {
		queries = append(queries,
			func() queryResult {
				return runQuery(searchCtx, fmt.Sprintf("ytsearch%d:%s", videoResultCount, query), 0, loc, mainRanking, false, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			},
			func() queryResult {
				// Adding "live" broadens discovery of currently broadcasting channels.
				// Full metadata still determines whether a result is actually live or a VOD.
				return runQuery(searchCtx, fmt.Sprintf("ytsearch%d:%s live", liveResultCount, query), 0, loc, liveRanking, true, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			},
			func() queryResult {
				return runPlaylistQuery(searchCtx, playlistSearchURL(query), playlistCount, loc, playlistRanking, &seen, &uniqueCount, liveJobs, emit)
			},
			func() queryResult {
				// yt-dlp's search never returns Shorts, so they come from the
				// Shorts results page.
				return runShortsQuery(searchCtx, query, Filters{Type: TypeShorts}, loc, shortResultCount, shortRanking(query), &seen, &uniqueCount, regularJobs, emit)
			},
		)
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
	close(regularJobs)
	close(liveJobs)

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
		if len(messages) > 0 {
			searchErr = errors.New(strings.Join(messages, "; "))
		} else {
			searchErr = errors.New("no results found")
		}
	} else if searchCtx.Err() != nil && ctx.Err() == nil {
		// Partial results are useful even if a slow search times out.
		searchErr = fmt.Errorf("search timed out; showing partial results")
	}
	emit(Event{Kind: EventSearchDone, Count: int(uniqueCount.Load()), Err: searchErr})

	workers.Wait()
	emit(Event{Kind: EventDetailsEnd, Count: int(uniqueCount.Load())})
}

// startDetailWorkers starts the bounded pool that looks up richer metadata for
// results: stream status, Shorts, description and age. Jobs on live are served
// first, and jobs are taken in batches that share one yt-dlp run. The pool ends
// when both channels are closed and empty.
func startDetailWorkers(ctx context.Context, regularJobs, liveJobs <-chan media.Video, loc locale.Locale, emit func(Event)) *sync.WaitGroup {
	var workers sync.WaitGroup
	for i := 0; i < metadataWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			regular, live := regularJobs, liveJobs
			for {
				batch := collectBatch(ctx, &regular, &live)
				if len(batch) == 0 {
					return
				}
				_ = FetchDetailsBatch(ctx, batch, loc, func(v media.Video) {
					if ctx.Err() == nil {
						emit(Event{Kind: EventDetails, Video: v})
					}
				})
			}
		}()
	}
	return &workers
}

// collectBatch waits for one job, then gathers more for a short time, up to
// detailBatchSize. A channel that is closed is set to nil. It returns nothing
// when there is no more work or ctx ends.
func collectBatch(ctx context.Context, regular, live *<-chan media.Video) []media.Video {
	var batch []media.Video
	take := func(v media.Video, ok bool, from *<-chan media.Video) {
		if !ok {
			*from = nil
			return
		}
		batch = append(batch, v)
	}
	// Prefer live-search candidates so their status is resolved early.
	for len(batch) == 0 {
		if *regular == nil && *live == nil {
			return nil
		}
		select {
		case v, ok := <-*live:
			take(v, ok, live)
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return nil
		case v, ok := <-*live:
			take(v, ok, live)
		case v, ok := <-*regular:
			take(v, ok, regular)
		}
	}
	timer := time.NewTimer(detailBatchWait)
	defer timer.Stop()
	for len(batch) < detailBatchSize && (*regular != nil || *live != nil) {
		select {
		case <-ctx.Done():
			return batch
		case <-timer.C:
			return batch
		case v, ok := <-*live:
			take(v, ok, live)
		case v, ok := <-*regular:
			take(v, ok, regular)
		}
	}
	return batch
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
	prioritizeLive bool,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	regularJobs chan<- media.Video,
	liveJobs chan<- media.Video,
	emit func(Event),
) queryResult {
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
		position := rows
		rows++
		if unavailable(video.Title) {
			continue
		}
		if order.onlyLive && !video.IsCurrentlyLive() && !video.IsUpcoming() {
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
			emit(Event{Kind: EventResult, Video: video, Count: count, Rank: order.at(position)})
			if prioritizeLive {
				select {
				case liveJobs <- video:
				case <-ctx.Done():
				}
			} else if position < eagerVideos {
				select {
				case regularJobs <- video:
				case <-ctx.Done():
				}
			}
		} else if prioritizeLive {
			// If the general query found this first, schedule another detail
			// lookup from the live-oriented queue to prioritize live status.
			select {
			case liveJobs <- video:
			case <-ctx.Done():
			}
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

// runPlaylistQuery emits playlist results and queues them for a detail lookup
// on the priority queue, so their size and first videos show up early.
func runPlaylistQuery(
	ctx context.Context,
	source string,
	limit int,
	loc locale.Locale,
	order ranking,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	jobs chan<- media.Video,
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
		if err := json.Unmarshal(scanner.Bytes(), &playlist); err != nil || playlist.ID == "" || !playlist.IsPlaylist() {
			continue
		}
		position := rows
		rows++
		if _, duplicate := seen.LoadOrStore(playlist.ID, struct{}{}); !duplicate {
			emit(Event{Kind: EventResult, Video: playlist, Count: int(uniqueCount.Add(1)), Rank: order.at(position)})
			select {
			case jobs <- playlist:
			case <-ctx.Done():
			}
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

// FetchDetailsBatch looks up the details of videos with as few yt-dlp runs as
// possible (one for the videos, one for the playlists) and calls found for
// each one as soon as it arrives. It returns an error only when yt-dlp could
// not run or gave nothing.
func FetchDetailsBatch(parent context.Context, videos []media.Video, loc locale.Locale, found func(media.Video)) error {
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
func fetchGroup(parent context.Context, group []media.Video, loc locale.Locale, found func(media.Video)) error {
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
			"--flat-playlist", "--dump-single-json", "--no-warnings", "--no-progress", "--ignore-errors",
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
