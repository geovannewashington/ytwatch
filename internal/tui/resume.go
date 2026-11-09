package tui

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/resume"
	"github.com/geovannewashington/ytwatch/internal/youtube"
)

// SetResume turns on the memory of where each video was left: mpv saves its
// position in dir when it quits, and plays from there the next time. Call it
// after SetConfig, only when the config allows it.
func (m *Model) SetResume(dir string) {
	m.resumeDir = dir
	m.progress = resume.Load(dir)

	// What is known of the videos that were played: the history.
	m.metaPath = resume.MetaPath(dir)
	m.meta = resume.LoadMeta(m.metaPath)
	if m.pruneMeta(time.Now()) {
		_ = resume.SaveMeta(m.metaPath, m.meta)
	}
}

// pruneMeta drops the history that is older than [history] max_age_days, and the
// oldest entries beyond max_entries, like the history of searches. A video that
// was only learned about from its position stays while the position does.
// It reports whether anything was dropped.
func (m *Model) pruneMeta(now time.Time) bool {
	recent := map[string]time.Time{}
	for _, left := range resume.Recent(m.resumeDir) {
		recent[left.ID] = left.At
	}
	playedAt := func(e resume.Entry) time.Time {
		if e.Played > 0 {
			return time.Unix(e.Played, 0)
		}

		return recent[e.ID] // zero when there is no position either
	}

	dropped := false
	maxAge := time.Duration(m.cfg.History.MaxAgeDays) * 24 * time.Hour
	for id, e := range m.meta {
		at := playedAt(e)
		if at.IsZero() || maxAge > 0 && now.Sub(at) > maxAge {
			delete(m.meta, id)
			dropped = true
		}
	}

	if limit := m.cfg.History.MaxEntries; limit > 0 && len(m.meta) > limit {
		entries := make([]resume.Entry, 0, len(m.meta))
		for _, e := range m.meta {
			entries = append(entries, e)
		}
		slices.SortFunc(entries, func(a, b resume.Entry) int { return playedAt(b).Compare(playedAt(a)) })
		for _, e := range entries[limit:] {
			delete(m.meta, e.ID)
		}
		dropped = true
	}

	return dropped
}

// refreshProgress reads the positions again, after mpv wrote one.
func (m *Model) refreshProgress() {
	if m.resumeDir != "" {
		m.progress = resume.Load(m.resumeDir)
	}
}

// rememberable tells whether the position in v is kept. A live stream, a
// Short and a playlist have no place to come back to.
func rememberable(v media.Video) bool {
	return v.ID != "" && !v.IsCurrentlyLive() && !v.IsUpcoming() && !v.IsShort() && !v.IsPlaylist()
}

func videoLength(v media.Video) time.Duration {
	return time.Duration(v.Duration * float64(time.Second))
}

// resumeOptions adds to opts what mpv needs to remember the position. A video
// that was left near its end starts over, as it counts as watched. When one
// of several videos cannot be resumed, none is: they share one mpv.
func (m *Model) resumeOptions(videos []media.Video, opts player.Options) player.Options {
	if m.resumeDir == "" {
		return opts
	}
	m.recordMeta(videos, m.now().Unix()) // the history has everything that is played

	for _, v := range videos {
		if !rememberable(v) {
			return opts
		}
	}
	opts.ResumeDir = m.resumeDir

	if len(videos) == 1 && !opts.StartOver {
		v := videos[0]
		if position := m.progress[v.ID]; position > 0 && !resume.Resumable(position, videoLength(v)) {
			opts.StartOver = true
		}
	}

	return opts
}

// resumeStatus is the line for a video that plays from where it was left, or "".
func (m *Model) resumeStatus(videos []media.Video, opts player.Options) string {
	if len(videos) != 1 || opts.ResumeDir == "" || opts.StartOver {
		return ""
	}

	v := videos[0]
	if position := m.progress[v.ID]; resume.Resumable(position, videoLength(v)) {
		return "Resuming at " + media.FormatDuration(position.Seconds())
	}

	return ""
}

// watchedFraction is how much of v was watched, from 0 to 1, and whether there
// is anything to show. A video left near its end counts as fully watched, and
// one of unknown length has no fraction.
func (m *Model) watchedFraction(v media.Video) (float64, bool) {
	position := m.progress[v.ID]
	length := videoLength(v)
	if position < resume.MinPosition && m.playedToEnd(v) {
		return 1, true // it played to its end, and mpv forgot the position
	}
	if position < resume.MinPosition || !rememberable(v) || length <= 0 {
		return 0, false
	}

	if !resume.Resumable(position, length) {
		return 1, true
	}

	return resume.Fraction(position, length), true
}

// listBar is the thin bar at the end of the second line of a row, like the red
// line under a YouTube thumbnail. It is "" when there is nothing to show.
func (m *Model) listBar(v media.Video) string {
	if v.IsPlaylist() {
		return ""
	}

	fraction, ok := m.watchedFraction(v)
	if !ok {
		return ""
	}

	return progressBar(fraction, listBarWidth)
}

// previewProgress is the bar of the preview with the place left and the length,
// or "". The bar fills width.
func (m *Model) previewProgress(v media.Video, width int) string {
	position := m.progress[v.ID]
	if position < resume.MinPosition || !rememberable(v) {
		return ""
	}

	text := "at " + media.FormatDuration(position.Seconds())
	fraction, ok := m.watchedFraction(v)
	if !ok {
		return metaStyle.Render(text)
	}

	text = media.FormatDuration(position.Seconds()) + " / " + media.FormatDuration(v.Duration)
	if fraction >= 1 {
		text = "watched"
	}

	room := width - lipgloss.Width(text) - 1
	if room < 4 {
		return metaStyle.Render(truncate(text, width))
	}

	return progressBar(fraction, room) + " " + metaStyle.Render(text)
}

// listBarWidth is the width of the bar of a row, in cells.
const listBarWidth = 8

// progressBar draws fraction of a bar of cells characters: red for what was
// watched, dim for the rest.
func progressBar(fraction float64, cells int) string {
	filled := min(cells, max(0, int(fraction*float64(cells)+0.5)))

	return progressStyle.Render(strings.Repeat("━", filled)) +
		metaStyle.Render(strings.Repeat("─", cells-filled))
}

// resumeBoost is how far a video that was started and not finished rises in the
// results of a search: 5 places of the main query, whose results are 10 apart.
// It stays small, so the order is still mostly YouTube's.
const resumeBoost = 50

// boostedRank is the rank of a result of a search. A video left halfway rises a
// few places, so it is easy to continue. The lists of a playlist or a channel
// keep their own order.
func (m *Model) boostedRank(v media.Video, rank int) int {
	if m.resultCap == 0 || !rememberable(v) {
		return rank
	}
	if resume.Resumable(m.progress[v.ID], videoLength(v)) {
		return rank - resumeBoost
	}

	return rank
}

// recordMeta keeps the title and channel of videos for the history. A stamp, in
// seconds since 1970, says when they were played; 0 keeps the time that is
// known. A playlist or a channel is not a video, and a result without a title is
// left for when its details arrive.
func (m *Model) recordMeta(videos []media.Video, stamp int64) {
	if m.metaPath == "" {
		return
	}
	if m.meta == nil {
		m.meta = map[string]resume.Entry{}
	}

	changed := false
	for _, v := range videos {
		title := clean(v.Title)
		if v.ID == "" || v.IsPlaylist() || v.IsChannel() || strings.TrimSpace(title) == "" {
			continue
		}

		channel := v.Channel
		if channel == "" {
			channel = v.Uploader
		}
		entry := resume.Entry{
			ID: v.ID, Title: title, Channel: clean(channel), Duration: v.Duration, Short: v.IsShort(),
			Played: m.meta[v.ID].Played,
		}
		if stamp > 0 {
			entry.Played = stamp
		}
		if old, ok := m.meta[v.ID]; ok && old == entry {
			continue
		}
		m.meta[v.ID], changed = entry, true
	}

	if changed {
		_ = resume.SaveMeta(m.metaPath, m.meta)
	}
}

// noteDetails records what the details of a video say about it, when it was left
// halfway and nothing is known of it yet: a video played before ytwatch kept
// this, which the history shows with its address only. Its time is the one of
// its position.
func (m *Model) noteDetails(id string) {
	if m.metaPath == "" || m.progress[id] <= 0 || m.meta[id].Title != "" {
		return
	}

	known := func(items []media.Video) (media.Video, bool) {
		for _, v := range items {
			if v.ID == id {
				return v, true
			}
		}

		return media.Video{}, false
	}
	v, ok := known(m.items)
	if !ok && m.narrowBase != nil {
		v, ok = known(m.narrowBase.items) // a filter may hide it
	}
	if !ok {
		return
	}

	var stamp int64
	if m.meta[id].Played == 0 {
		for _, left := range resume.Recent(m.resumeDir) {
			if left.ID == id {
				stamp = left.At.Unix()
			}
		}
	}
	m.recordMeta([]media.Video{v}, stamp)
}

// playedToEnd reports whether v, which is in the history, played to its end: mpv
// forgot its position, and enough time has passed since it was played for it to
// have been watched, which is half its length, or 5 minutes when the length is
// not known. A video that was cut short by a crash has no position either, and
// must not show as watched.
func (m *Model) playedToEnd(v media.Video) bool {
	entry, ok := m.meta[v.ID]
	if !ok || entry.Played == 0 || !rememberable(v) {
		return false
	}
	if _, hasPosition := m.progress[v.ID]; hasPosition {
		return false
	}
	if _, playing := m.playing[v.ID]; playing {
		return false
	}

	needed := 5 * time.Minute
	if entry.Duration > 0 {
		needed = time.Duration(entry.Duration*float64(time.Second)) / 2
	}

	return m.now().Sub(time.Unix(entry.Played, 0)) >= needed
}

// leftHalfway lists the videos of the history that were left halfway, the last
// one played first. It uses what is in memory: it runs at every search.
func (m *Model) leftHalfway() []media.Video {
	type item struct {
		video  media.Video
		played int64
	}
	var items []item
	for id, entry := range m.meta {
		position, ok := m.progress[id]
		if !ok {
			continue
		}

		address := "https://www.youtube.com/watch?v=" + id
		v := media.Video{
			ID: id, Title: entry.Title, Channel: entry.Channel, Duration: entry.Duration,
			URL: address, WebpageURL: address,
		}
		if rememberable(v) && resume.Resumable(position, videoLength(v)) {
			items = append(items, item{v, entry.Played})
		}
	}

	slices.SortFunc(items, func(a, b item) int {
		if a.played != b.played {
			return int(b.played - a.played)
		}

		return strings.Compare(a.video.ID, b.video.ID)
	})

	videos := make([]media.Video, len(items))
	for i, it := range items {
		videos[i] = it.video
	}

	return videos
}

// maxLeftInSearch is how many videos left halfway a search brings back.
const maxLeftInSearch = 5

// leftMatching are the videos left halfway that the search is about: each word
// of the query is in the title or the channel. Only videos that ytwatch knows
// the title of can match.
func (m *Model) leftMatching(query string) []media.Video {
	var found []media.Video
	for _, v := range m.leftHalfway() {
		if v.Title != "" && containsAllWords(v.Title+" "+v.Channel, query) {
			found = append(found, v)
			if len(found) == maxLeftInSearch {
				break
			}
		}
	}

	return found
}

// containsAllWords reports whether every word of query is in text, ignoring
// case, accents and punctuation. It is stricter than the filter of the results:
// a video comes back into a search only when it really is about it.
func containsAllWords(text, query string) bool {
	words := strings.FieldsFunc(foldText(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(words) == 0 {
		return false
	}

	haystack := foldText(text)
	for _, word := range words {
		if !strings.Contains(haystack, word) {
			return false
		}
	}

	return true
}

// historyVideos lists what was played, the last one first: the videos of the
// history file, and the ones that were only learned about from a position. A
// video that has no position and was not left by mpv playing is one that ended.
func (m *Model) historyVideos() []media.Video {
	if m.resumeDir == "" {
		return nil
	}

	positions := map[string]resume.Left{}
	for _, left := range resume.Recent(m.resumeDir) {
		positions[left.ID] = left
	}

	type item struct {
		video media.Video
		at    time.Time
	}
	var items []item
	address := func(id string) string { return "https://www.youtube.com/watch?v=" + id }

	for id, e := range m.meta {
		at := time.Unix(e.Played, 0)
		if left, ok := positions[id]; ok && e.Played == 0 {
			at = left.At
		}
		v := media.Video{
			ID: id, Title: e.Title, Channel: e.Channel, Duration: e.Duration, Short: e.Short,
			URL: address(id), WebpageURL: address(id),
		}
		items = append(items, item{v, at})
	}
	for id, left := range positions {
		if _, known := m.meta[id]; !known {
			v := media.Video{ID: id, URL: address(id), WebpageURL: address(id)}
			items = append(items, item{v, left.At})
		}
	}

	slices.SortFunc(items, func(a, b item) int { return b.at.Compare(a.at) })
	if limit := m.cfg.History.MaxEntries; limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	videos := make([]media.Video, len(items))
	for i, it := range items {
		videos[i] = it.video
	}

	return videos
}

// openHistory lists the videos that were played, the last one first, each with
// how far it was watched. Esc returns to what was on screen.
func (m *Model) openHistory() tea.Cmd {
	if m.resumeDir == "" {
		m.status = "Watch history is off" + noteMark + "see [history] in config.toml"
		return nil
	}

	m.refreshProgress()
	videos := m.historyVideos()
	if len(videos) == 0 {
		m.status = "No history yet" + noteMark + "the videos you play are listed here"
		return nil
	}

	m.clearNarrow()
	m.pushResults()
	m.channel = nil
	m.listTitle = "History"

	return m.startStream("Opening...", func(_ context.Context, emit func(youtube.Event)) {
		for i, v := range videos {
			emit(youtube.Event{Kind: youtube.EventResult, Video: v, Count: i + 1, Rank: i * 10})
		}
		emit(youtube.Event{Kind: youtube.EventSearchDone, Count: len(videos)})
		emit(youtube.Event{Kind: youtube.EventDetailsEnd, Count: len(videos)})
	})
}
