package youtube

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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
	metadataWorkers  = 4
)

type Event struct {
	Kind  string
	Video media.Video
	Count int
	Err   error
}

type queryResult struct {
	rows int
	err  error
}

// Search runs a normal relevance search alongside a live-oriented search.
// Both stream flat results immediately; richer metadata is resolved by a
// bounded worker pool so stream status and Shorts details can update later.
func Search(ctx context.Context, query string, emit func(Event)) {
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
	queryResults := make(chan queryResult, 2)

	searchWG.Add(2)
	go func() {
		defer searchWG.Done()
		queryResults <- runQuery(searchCtx, query, videoResultCount, false, &seen, &uniqueCount, regularJobs, liveJobs, emit)
	}()
	go func() {
		defer searchWG.Done()
		// Adding "live" broadens discovery of currently broadcasting channels.
		// Full metadata still determines whether a result is actually live or a VOD.
		queryResults <- runQuery(searchCtx, query+" live", liveResultCount, true, &seen, &uniqueCount, regularJobs, liveJobs, emit)
	}()

	searchWG.Wait()
	close(regularJobs)
	close(liveJobs)

	results := []queryResult{<-queryResults, <-queryResults}
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

func runQuery(
	ctx context.Context,
	query string,
	limit int,
	prioritizeLive bool,
	seen *sync.Map,
	uniqueCount *atomic.Int64,
	regularJobs chan<- media.Video,
	liveJobs chan<- media.Video,
	emit func(Event),
) queryResult {
	searchURL := fmt.Sprintf("ytsearch%d:%s", limit, query)
	args := []string{
		"--flat-playlist", "--dump-json", "--no-warnings", "--ignore-errors",
		"--skip-download", "--socket-timeout", "7",
		"--extractor-retries", "1", "--retries", "1",
		"--extractor-args", "youtubetab:approximate_date",
		searchURL,
	}
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
		rows++
		if video.Title == "" {
			video.Title = "Untitled video"
		}
		if video.WebpageURL == "" && video.OriginalURL == "" && video.URL == "" {
			video.WebpageURL = video.CanonicalURL()
		}

		_, duplicate := seen.LoadOrStore(video.ID, struct{}{})
		if !duplicate {
			count := int(uniqueCount.Add(1))
			emit(Event{Kind: EventResult, Video: video, Count: count})
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

func FetchDetails(parent context.Context, base media.Video) (media.Video, error) {
	url := base.CanonicalURL()
	if url == "" {
		return base, errors.New("result has no usable URL")
	}
	ctx, cancel := context.WithTimeout(parent, 9*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--dump-single-json", "--no-warnings", "--no-progress", "--skip-download",
		"--no-playlist", "--ignore-no-formats-error",
		"--socket-timeout", "6", "--extractor-retries", "1", "--retries", "1", url)
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
