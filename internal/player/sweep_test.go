package player

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepStaleRemovesOnlyOldFoldersOfYtwatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	old, fresh := filepath.Join(tmp, workDirPrefix+"old"), filepath.Join(tmp, workDirPrefix+"fresh")
	other, file := filepath.Join(tmp, "other-old"), filepath.Join(tmp, workDirPrefix+"file")
	for _, dir := range []string{old, fresh, other} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(old, "sheet-0.bgra"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(tmp, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, workDirPrefix+"link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	week := time.Now().Add(-7 * 24 * time.Hour)
	for _, path := range []string{old, other, file, target} {
		if err := os.Chtimes(path, week, week); err != nil {
			t.Fatal(err)
		}
	}

	SweepStale(24*time.Hour, time.Now())

	for path, want := range map[string]bool{old: false, fresh: true, other: true, file: true, link: true, target: true} {
		_, err := os.Lstat(path)
		if (err == nil) != want {
			t.Errorf("%s: exists = %v, want %v", filepath.Base(path), err == nil, want)
		}
	}
}
