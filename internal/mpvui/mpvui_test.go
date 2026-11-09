package mpvui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func privateDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ytwatch-ui-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	return dir
}

func TestPrepareWritesTheScriptAndTheDataAndGivesTheArguments(t *testing.T) {
	dir := privateDir(t)
	in := Input{Title: "A  video\x07 title", Duration: 213 * time.Second, Heatmap: []float64{1, 0.5, 0}}

	args, done, err := Prepare(context.Background(), dir, in)
	if err != nil {
		t.Fatal(err)
	}
	<-done

	want := []string{
		"--osc=no", "--osd-bar=no",
		"--script=" + filepath.Join(dir, "ytwatch-ui.lua"),
		"--script-opts-append=ytwatch-ui-data=" + filepath.Join(dir, "ytwatch-ui.json"),
	}
	if strings.Join(args, "\n") != strings.Join(want, "\n") {
		t.Errorf("args = %q, want %q", args, want)
	}

	for _, name := range []string{"ytwatch-ui.lua", "ytwatch-ui.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v", name, info, err)
		}
	}

	var got fileData
	raw, _ := os.ReadFile(filepath.Join(dir, "ytwatch-ui.json"))
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "A  video  title" || got.Duration != 213 || len(got.Heatmap) != 3 || got.Storyboard != nil {
		t.Errorf("data = %+v", got)
	}

	script, _ := os.ReadFile(filepath.Join(dir, "ytwatch-ui.lua"))
	if !bytes.Equal(script, scriptSource()) || !strings.Contains(string(script), "ytwatch-ui") {
		t.Error("the script of the binary must be the one that is written")
	}
}

func scriptSource() []byte { return script }

func TestPrepareWithNothingKnownStillWorks(t *testing.T) {
	dir := privateDir(t)

	args, done, err := Prepare(context.Background(), dir, Input{})
	if err != nil || len(args) != 4 {
		t.Fatalf("args = %q, err = %v", args, err)
	}
	<-done

	raw, _ := os.ReadFile(filepath.Join(dir, "ytwatch-ui.json"))
	if string(raw) != "{}" {
		t.Errorf("no title, graph or pictures: %s", raw)
	}
}

func TestPrepareDescribesThePicturesAndFetchesThemOnlyFromYouTube(t *testing.T) {
	dir := privateDir(t)
	// A spec that points outside YouTube: it is described, and nothing is fetched.
	spec := "https://evil.example/sb/x/s_L$L/$N.jpg?a=b|160#90#108#5#5#2000#M$M#sigh"

	_, done, err := Prepare(context.Background(), dir, Input{Duration: 213 * time.Second, StoryboardSpec: spec})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the download must end")
	}

	var got fileData
	raw, _ := os.ReadFile(filepath.Join(dir, "ytwatch-ui.json"))
	_ = json.Unmarshal(raw, &got)
	if b := got.Storyboard; b == nil || b.Width != 320 || b.Height != 180 || b.Cols != 5 || b.Rows != 5 || b.Interval != 2000 || b.Count != 108 || b.Dir != dir {
		t.Errorf("storyboard = %+v", got.Storyboard)
	}

	if files, _ := filepath.Glob(filepath.Join(dir, "sheet-*")); len(files) != 0 {
		t.Errorf("nothing may be fetched from outside YouTube: %v", files)
	}

	// A broken spec is no pictures, not an error.
	_, done, err = Prepare(context.Background(), privateDir(t), Input{StoryboardSpec: "nonsense"})
	if err != nil {
		t.Errorf("a broken spec must not stop the controls: %v", err)
	}
	<-done
}

func TestPrepareStopsDownloadingWhenTheContextEnds(t *testing.T) {
	dir := privateDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	spec := "https://i.ytimg.com/sb/x/s_L$L/$N.jpg?a=b|160#90#108#5#5#2000#M$M#sigh"
	_, done, err := Prepare(ctx, dir, Input{Duration: time.Minute, StoryboardSpec: spec})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the download must stop when the context ends")
	}
}

func TestPrepareRefusesAFolderMpvCannotHoldInAnOption(t *testing.T) {
	for _, dir := range []string{"", "/tmp/a,b", "/tmp/a=b", "/tmp/a b", "/tmp/a%b", "/tmp/a\"b", "/tmp/a\nb"} {
		if _, _, err := Prepare(context.Background(), dir, Input{}); err == nil {
			t.Errorf("%q should be refused", dir)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	if got := cleanTitle("  a\nb\x1b[31m\u0085c  "); got != "a b [31m c" {
		t.Errorf("cleanTitle = %q", got)
	}
	if got := cleanTitle(strings.Repeat("é", 500)); len([]rune(got)) != maxTitle {
		t.Errorf("the title must be cut, got %d characters", len([]rune(got)))
	}
}

// The script runs in a real mpv, with data of every kind. It must load, draw
// and say nothing: mpv writes a script's errors to its log.
func TestTheScriptRunsInMpvWithoutErrors(t *testing.T) {
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv is not installed")
	}

	for name, data := range map[string]string{
		"full":    `{"title":"A title","duration":60,"heatmap":[0,0.5,1,0.5,0],"storyboard":{"width":160,"height":90,"cols":5,"rows":5,"interval":2000,"count":30,"dir":"/nonexistent"}}`,
		"empty":   `{}`,
		"broken":  `not json {{{`,
		"strange": `{"title":123,"duration":"x","heatmap":"nope","storyboard":{"width":"a","cols":-1}}`,
		"hostile": `{"title":"{\\fs300}\\N{\\p1}","heatmap":[1,"a",null,{},[1],2,-9,1e999],"storyboard":{"width":99999999}}`,
	} {
		dir := privateDir(t)
		if err := os.WriteFile(filepath.Join(dir, "ytwatch-ui.lua"), script, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ytwatch-ui.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
		cmd := exec.CommandContext(ctx, mpv, "--no-config", "--no-audio", "--vo=null", "--osc=no", "--pause", "--msg-level=all=error",
			"--script="+filepath.Join(dir, "ytwatch-ui.lua"),
			"--script-opts=ytwatch-ui-data="+filepath.Join(dir, "ytwatch-ui.json")+",ytwatch-ui-size=1280x720,ytwatch-ui-dump="+filepath.Join(dir, "frame.ass"),
			"av://lavfi:color=c=black:s=64x64:r=5:d=60")
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		_ = cmd.Run() // it is stopped by the timeout
		cancel()

		if strings.Contains(output.String(), "rror") {
			t.Errorf("%s: mpv reported an error:\n%s", name, output.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "frame.ass")); err != nil {
			t.Errorf("%s: the script drew nothing: %v\n%s", name, err, output.String())
		}
	}
}

func TestSafeOptionRefusesWhatSplitsAnOption(t *testing.T) {
	for _, path := range []string{"/tmp/a,b", "/tmp/a=b", "/tmp/a:b", "/tmp/a b", "", "/tmp/a'b"} {
		if safeOption(path) {
			t.Errorf("%q must be refused", path)
		}
	}
	if !safeOption("/tmp/ytwatch-mpv-123") {
		t.Error("a plain path is fine")
	}
}

func TestTheScriptSavesThePositionNowAndThenWhenAskedTo(t *testing.T) {
	src := string(script)
	for _, want := range []string{"write-watch-later-config", "save-position-on-quit", "add_periodic_timer(30"} {
		if !strings.Contains(src, want) {
			t.Errorf("the script should contain %q", want)
		}
	}
}
