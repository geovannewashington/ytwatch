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
	"time"

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

	// A filtered search is one query on the results page.
	filteredResultCount   = 50
	filteredPlaylistCount = 30
	metadataWorkers       = 4
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

// The main query's results are 10 apart, so a playlist (rank 80, then every
// 40) lands after the 8th result, then after every 4th.
var (
	mainRanking     = ranking{base: 0, step: 10}
	playlistRanking = ranking{base: 80, step: 40}
	liveRanking     = ranking{base: 360, step: 10, onlyLive: true}
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
func Search(ctx context.Context, query string, filters Filters, emit func(Event)) {
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
	var workers sync.WaitGroup
	for i := 0; i < metadataWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			regular := (<-chan media.Video)(regularJobs)
			live := (<-chan media.Video)(liveJobs)
			for regular != nil || live != nil {
				// Prefer live-search candidates so their status is resolved early.
				select {
				case v, ok := <-live:
					if !ok {
						live = nil
						continue
					}
					resolveDetails(ctx, v, emit)
					continue
				default:
				}
				select {
				case <-ctx.Done():
					return
				case v, ok := <-live:
					if !ok {
						live = nil
					} else {
						resolveDetails(ctx, v, emit)
					}
				case v, ok := <-regular:
					if !ok {
						regular = nil
					} else {
						resolveDetails(ctx, v, emit)
					}
				}
			}
		}()
	}

	var seen sync.Map
	var uniqueCount atomic.Int64
	var searchWG sync.WaitGroup
	var queries []func() queryResult
	if filters.Active() {
		if filters.Type == TypeShorts {
			queries = append(queries, func() queryResult {
				return runShortsQuery(searchCtx, query, filters, &seen, &uniqueCount, regularJobs, emit)
			})
		} else if filters.Type == TypePlaylists {
			queries = append(queries, func() queryResult {
				return runPlaylistQuery(searchCtx, resultsURL(query, filters), filteredPlaylistCount, mainRanking, &seen, &uniqueCount, liveJobs, emit)
			})
		} else {
			queries = append(queries, func() queryResult {
				return runQuery(searchCtx, resultsURL(query, filters), filteredResultCount, mainRanking, filters.Type == TypeLive, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			})
		}
	} else {
		queries = append(queries,
			func() queryResult {
				return runQuery(searchCtx, fmt.Sprintf("ytsearch%d:%s", videoResultCount, query), 0, mainRanking, false, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			},
			func() queryResult {
				// Adding "live" broadens discovery of currently broadcasting channels.
				// Full metadata still determines whether a result is actually live or a VOD.
				return runQuery(searchCtx, fmt.Sprintf("ytsearch%d:%s live", liveResultCount, query), 0, liveRanking, true, &seen, &uniqueCount, regularJobs, liveJobs, emit)
			},
			func() queryResult {
				return runPlaylistQuery(searchCtx, playlistSearchURL(query), playlistCount, playlistRanking, &seen, &uniqueCount, liveJobs, emit)
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

// runQuery runs one yt-dlp search and streams its results. source is a
// "ytsearchN:query" spec or a results-page URL; listEnd limits how many
// entries a URL returns (a ytsearch spec carries its own count, so it is 0).
func runQuery(
	ctx context.Context,
	source string,
	listEnd int,
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
			} else {
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
	order ranking,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	jobs chan<- media.Video,
	emit func(Event),
) queryResult {
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--flat-playlist", "--dump-json", "--no-warnings", "--ignore-errors",
		"--skip-download", "--socket-timeout", "7",
		"--extractor-retries", "1", "--retries", "1",
		"--playlist-end", strconv.Itoa(limit),
		source)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return queryResult{err: err}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return queryResult{err: fmt.Errorf("start yt-dlp: %w", err)}
	}

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

func resolveDetails(ctx context.Context, video media.Video, emit func(Event)) {
	if ctx.Err() != nil {
		return
	}
	details, err := FetchDetails(ctx, video)
	if err != nil {
		return
	}
	select {
	case <-ctx.Done():
	default:
		emit(Event{Kind: EventDetails, Video: details})
	}
}

// playlistPreviewVideos is how many videos a playlist lookup lists.
const playlistPreviewVideos = 3

func FetchDetails(parent context.Context, base media.Video) (media.Video, error) {
	url := base.CanonicalURL()
	if url == "" {
		return base, errors.New("result has no usable URL")
	}
	ctx, cancel := context.WithTimeout(parent, 9*time.Second)
	defer cancel()

	args := []string{
		"--dump-single-json", "--no-warnings", "--no-progress", "--skip-download",
		"--no-playlist", "--ignore-no-formats-error",
		"--socket-timeout", "6", "--extractor-retries", "1", "--retries", "1", url,
	}
	if base.IsPlaylist() {
		// A flat lookup of the first videos is quick and still reports the
		// playlist's size, update date and description.
		args = []string{
			"--flat-playlist", "--dump-single-json", "--no-warnings", "--no-progress",
			"--playlist-end", strconv.Itoa(playlistPreviewVideos),
			"--socket-timeout", "6", "--extractor-retries", "1", "--retries", "1", url,
		}
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return base, ctx.Err()
		}
		message := lastUsefulLine(stderr.String())
		if message == "" {
			return base, err
		}
		return base, errors.New(message)
	}
	var details media.Video
	if err := json.Unmarshal(stdout.Bytes(), &details); err != nil {
		return base, err
	}
	return media.MergeDetails(base, details), nil
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
