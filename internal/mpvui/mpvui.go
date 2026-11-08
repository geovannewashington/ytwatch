// Package mpvui gives mpv the controls of the YouTube player: a progress bar
// with the "most replayed" graph and the small pictures of the video under the
// mouse, and the usual buttons. The drawing is done by a script that runs in
// mpv (ytwatch-ui.lua). This package gives it what it needs for one video and
// the mpv arguments that load it.
package mpvui

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/geovannewashington/ytwatch/internal/storyboard"
)

// script is the Lua script that draws the controls.
//
//go:embed ytwatch-ui.lua
var script []byte

const (
	scriptName = "ytwatch-ui.lua"
	dataName   = "ytwatch-ui.json"

	maxTitle = 200
)

// Input is what is known of the video that plays. Everything is optional: with
// nothing the controls still work, without the graph and the pictures.
type Input struct {
	Title    string
	Duration time.Duration
	// Heatmap is the "most replayed" graph, scores from 0 to 1.
	Heatmap []float64
	// StoryboardSpec is the description of the pictures from the watch page.
	StoryboardSpec string
}

type boardData struct {
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Interval int    `json:"interval"`
	Count    int    `json:"count"`
	Dir      string `json:"dir"`
}

type fileData struct {
	Title      string     `json:"title,omitempty"`
	Duration   float64    `json:"duration,omitempty"`
	Heatmap    []float64  `json:"heatmap,omitempty"`
	Storyboard *boardData `json:"storyboard,omitempty"`
}

// Prepare writes the script and the data of the video into dir, which is a
// folder only the user can open, and returns the arguments that make mpv use
// them. The pictures of the video are downloaded in the background into dir, and
// the returned channel is closed when that is over, which is soon after ctx
// ends. mpv draws a picture once its file is there.
func Prepare(ctx context.Context, dir string, in Input) (args []string, done <-chan struct{}, err error) {
	// The paths go into an mpv option, which is split at commas and equal signs.
	if !safeOption(dir) {
		return nil, nil, errors.New("the folder of the controls has a character that mpv options cannot hold")
	}

	data := fileData{Title: cleanTitle(in.Title), Duration: in.Duration.Seconds(), Heatmap: in.Heatmap}

	var spec storyboard.Spec
	var level int
	if parsed, err := storyboard.Parse(in.StoryboardSpec); err == nil {
		if index, chosen, ok := parsed.Choose(in.Duration); ok {
			spec, level = parsed, index
			width, height := storyboard.FrameSize(chosen)
			data.Storyboard = &boardData{
				Width: width, Height: height, Cols: chosen.Cols, Rows: chosen.Rows,
				Interval: chosen.Interval, Count: chosen.Count, Dir: dir,
			}
		}
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}

	scriptPath, dataPath := filepath.Join(dir, scriptName), filepath.Join(dir, dataName)
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(dataPath, encoded, 0o600); err != nil {
		return nil, nil, err
	}

	finished := make(chan struct{})
	if data.Storyboard != nil {
		go func() {
			defer close(finished)
			storyboard.Download(ctx, spec, level, dir)
		}()
	} else {
		close(finished)
	}

	return []string{
		// The controls of mpv are replaced, and so is the bar it draws when the
		// volume or the position changes.
		"--osc=no",
		"--osd-bar=no",
		"--script=" + scriptPath,
		"--script-opts-append=ytwatch-ui-data=" + dataPath,
	}, finished, nil
}

// safeOption reports whether a path can be put in an mpv option as it is. A
// colon splits a list of scripts, and a comma or an equal sign splits options.
func safeOption(path string) bool {
	return path != "" && !strings.ContainsAny(path, ",=%:\"'\n\r\t ") && utf8.ValidString(path)
}

// cleanTitle makes the title short and free of control characters. The script
// checks what it draws too, but there is no reason to hand it more than it
// needs.
func cleanTitle(title string) string {
	var b strings.Builder
	count := 0

	for _, r := range title {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			r = ' '
		}
		if count == maxTitle {
			break
		}
		b.WriteRune(r)
		count++
	}

	return strings.TrimSpace(b.String())
}
