package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersionAndNewer(t *testing.T) {
	for text, want := range map[string]string{"0.2.0": "0.2.0", "v1.10.3": "1.10.3", " v0.1.0\n": "0.1.0"} {
		v, err := ParseVersion(text)
		if err != nil || v.String() != want {
			t.Errorf("ParseVersion(%q) = %v, %v", text, v, err)
		}
	}
	for _, bad := range []string{"", "dev", "v1.2", "v1.2.3-rc1", "1.2.3.4", "v1.2.x", "../../etc", "v99999.0.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q must not be a version", bad)
		}
	}

	a, _ := ParseVersion("0.2.0")
	b, _ := ParseVersion("0.10.0")
	c, _ := ParseVersion("0.2.0")
	if !b.Newer(a) || a.Newer(b) || a.Newer(c) {
		t.Error("versions are compared by number, not by text")
	}
}

func TestArchiveName(t *testing.T) {
	v, _ := ParseVersion("0.2.0")
	if name, err := ArchiveName(v, "arm64"); err != nil || name != "ytwatch_0.2.0_linux_arm64.tar.gz" {
		t.Errorf("name = %q, %v", name, err)
	}
	if _, err := ArchiveName(v, "riscv64"); err == nil {
		t.Error("an unknown processor has no release")
	}
}

func TestWantedSum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	text := []byte(sum + "  ytwatch_0.2.0_linux_amd64.tar.gz\n" + strings.Repeat("0", 64) + "  other.tar.gz\n")

	if got, err := WantedSum(text, "ytwatch_0.2.0_linux_amd64.tar.gz"); err != nil || got != sum {
		t.Errorf("sum = %q, %v", got, err)
	}
	if _, err := WantedSum(text, "ytwatch_0.2.0_linux_arm64.tar.gz"); err == nil {
		t.Error("a file that is not listed has no sum")
	}
	if _, err := WantedSum([]byte("short  ytwatch_0.2.0_linux_amd64.tar.gz\n"), "ytwatch_0.2.0_linux_amd64.tar.gz"); err == nil {
		t.Error("a sum that is not 64 hex digits is refused")
	}
}

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var out bytes.Buffer
	zipped := gzip.NewWriter(&out)
	writer := tar.NewWriter(zipped)
	for name, body := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func TestProgramTakesOnlyYtwatch(t *testing.T) {
	got, err := Program(archive(t, map[string]string{"README.md": "x", "../../evil": "bad", "./ytwatch": "program"}))
	if err != nil || string(got) != "program" {
		t.Errorf("Program = %q, %v", got, err)
	}
	if _, err := Program(archive(t, map[string]string{"LICENSE": "x"})); err == nil {
		t.Error("an archive without ytwatch is refused")
	}
	if _, err := Program([]byte("not an archive")); err == nil {
		t.Error("garbage is refused")
	}
}

// release serves a fake release 0.2.0 on the loopback and points the package at it.
func release(t *testing.T, program string, sum func(archive []byte) string) {
	t.Helper()

	name := "ytwatch_0.2.0_linux_amd64.tar.gz"
	data := archive(t, map[string]string{"ytwatch": program, "LICENSE": "x"})

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/v0.2.0", http.StatusFound)
	})
	mux.HandleFunc("/download/v0.2.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sum(data) + "  " + name + "\n"))
	})
	mux.HandleFunc("/download/v0.2.0/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(data) })

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	oldReleases, oldAllowed := releases, allowedHost
	releases, allowedHost = server.URL, func(host string) bool { return host == "127.0.0.1" }
	t.Cleanup(func() { releases, allowedHost = oldReleases, oldAllowed })
}

func realSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestLatestReadsTheTagFromTheRedirect(t *testing.T) {
	release(t, "p", realSum)

	v, err := Latest(context.Background())
	if err != nil || v.String() != "0.2.0" {
		t.Errorf("Latest = %v, %v", v, err)
	}
}

func TestDownloadChecksTheSum(t *testing.T) {
	v, _ := ParseVersion("0.2.0")

	release(t, "new program", realSum)
	got, err := Download(context.Background(), v, "amd64")
	if err != nil || string(got) != "new program" {
		t.Fatalf("Download = %q, %v", got, err)
	}

	release(t, "new program", func([]byte) string { return strings.Repeat("0", 64) })
	if _, err := Download(context.Background(), v, "amd64"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("a wrong checksum must stop the update: %v", err)
	}
}

func TestReplaceSwapsTheFileAndKeepsItWorking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ytwatch")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "new" || info.Mode().Perm() != 0o755 {
		t.Errorf("content %q, mode %v", data, info.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ytwatch-update-*")); len(left) != 0 {
		t.Errorf("no temporary file may stay: %v", left)
	}

	if err := Replace(filepath.Join(dir, "nowhere", "ytwatch"), []byte("x")); err == nil {
		t.Error("a folder that does not exist is an error")
	}
}
