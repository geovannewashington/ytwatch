package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/geovannewashington/ytwatch/internal/media"
	"github.com/geovannewashington/ytwatch/internal/player"
	"github.com/geovannewashington/ytwatch/internal/resume"
)

// SetResume turns on the memory of where each video was left: mpv saves its
// position in dir when it quits, and plays from there the next time. Call it
// after SetConfig, only when the config allows it.
func (m *Model) SetResume(dir string) {
	m.resumeDir = dir
	m.progress = resume.Load(dir)
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
