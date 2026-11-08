// Package update finds the newest release of ytwatch and replaces the running
// program with it. Nothing here runs by itself: it is for "ytwatch update".
//
// What it trusts: the files come from GitHub over https, and the archive is
// accepted only when its SHA-256 matches the one in checksums.txt of the same
// release. That catches a broken or cut download. It does not protect against
// someone who controls the release itself, because the checksums are in the
// same place as the program.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// releasesURL is where the releases of ytwatch are.
	releasesURL = "https://github.com/geovannewashington/ytwatch/releases"

	maxChecksums = 1 << 20   // checksums.txt is a few hundred bytes
	maxArchive   = 100 << 20 // the archive is about 6 MB
	maxProgram   = 200 << 20 // the program inside it
	timeout      = 2 * time.Minute
)

var (
	// releases is a variable so the tests can use their own server.
	releases = releasesURL

	// allowedHost says where a download may come from: GitHub and the servers
	// it sends release files from. A test replaces it.
	allowedHost = func(host string) bool {
		return host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
	}

	tagPattern = regexp.MustCompile(`^v(\d{1,4})\.(\d{1,4})\.(\d{1,4})$`)
)

func client() *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 || next.URL.Scheme != "https" && !testScheme(next.URL) || !allowedHost(next.URL.Hostname()) {
				return errors.New("redirect to a place that is not allowed")
			}

			return nil
		},
	}
}

// testScheme lets the tests use http on the loopback. It is false in the real
// program, because allowedHost only accepts GitHub there.
func testScheme(u *url.URL) bool { return u.Scheme == "http" && u.Hostname() == "127.0.0.1" }

// Version is a release number such as 0.2.0.
type Version [3]int

// ParseVersion reads "0.2.0" or "v0.2.0". A pre-release or anything else is an
// error: only plain releases are offered.
func ParseVersion(text string) (Version, error) {
	m := tagPattern.FindStringSubmatch("v" + strings.TrimPrefix(strings.TrimSpace(text), "v"))
	if m == nil {
		return Version{}, fmt.Errorf("not a release version: %q", text)
	}

	var v Version
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}

	return v, nil
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Newer reports whether v comes after other.
func (v Version) Newer(other Version) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] > other[i]
		}
	}

	return false
}

// Latest asks GitHub for the newest release. The page "releases/latest" sends
// the browser to "releases/tag/<tag>", and the tag is read from there, so no
// API (and no rate limit) is needed.
func Latest(ctx context.Context) (Version, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releases+"/latest", nil)
	if err != nil {
		return Version{}, err
	}

	c := client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	response, err := c.Do(request)
	if err != nil {
		return Version{}, err
	}
	defer response.Body.Close()

	location, err := response.Location()
	if err != nil {
		return Version{}, errors.New("GitHub did not say which release is the latest")
	}

	return ParseVersion(filepath.Base(location.Path))
}

// ArchiveName is the name of the download for this computer.
func ArchiveName(v Version, arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("no release for %s (x86-64 and ARM64 are supported)", arch)
	}

	return fmt.Sprintf("ytwatch_%s_linux_%s.tar.gz", v, arch), nil
}

func get(ctx context.Context, address string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}

	response, err := client().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the download is bigger than expected")
	}

	return data, nil
}

// WantedSum finds the SHA-256 of name in the text of checksums.txt, whose lines
// are "<sha256>  <file name>".
func WantedSum(checksums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			if len(fields[0]) != 64 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				break
			}

			return strings.ToLower(fields[0]), nil
		}
	}

	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// Program takes the file "ytwatch" out of a .tar.gz. Nothing else in the
// archive is read or written, so a name in it cannot reach another place.
func Program(archive []byte) ([]byte, error) {
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}

	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive has no ytwatch in it")
		}
		if err != nil {
			return nil, err
		}

		if header.Typeflag != tar.TypeReg || strings.TrimPrefix(header.Name, "./") != "ytwatch" {
			continue
		}

		data, err := io.ReadAll(io.LimitReader(reader, maxProgram+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxProgram || len(data) == 0 {
			return nil, errors.New("the program in the archive has an unexpected size")
		}

		return data, nil
	}
}

// Download gets the release v, checks it and returns the new program. It
// changes nothing on disk.
func Download(ctx context.Context, v Version, arch string) ([]byte, error) {
	name, err := ArchiveName(v, arch)
	if err != nil {
		return nil, err
	}

	base := releases + "/download/v" + v.String() + "/"

	checksums, err := get(ctx, base+"checksums.txt", maxChecksums)
	if err != nil {
		return nil, fmt.Errorf("checksums: %w", err)
	}

	want, err := WantedSum(checksums, name)
	if err != nil {
		return nil, err
	}

	archive, err := get(ctx, base+name, maxArchive)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("the checksum of %s does not match (wanted %s, got %s): nothing was changed", name, want, got)
	}

	return Program(archive)
}

// Replace puts program where the file path is. It is written next to it under
// another name and then renamed over it, so a running ytwatch (or a copy that
// mpv's chat is using) is not cut in half, and a failure leaves the old one.
func Replace(path string, program []byte) error {
	dir := filepath.Dir(path)

	file, err := os.CreateTemp(dir, ".ytwatch-update-*")
	if err != nil {
		return fmt.Errorf("cannot write in %s: %w", dir, err)
	}
	temporary := file.Name()
	defer os.Remove(temporary) // after a rename it is gone, and this does nothing

	if _, err := file.Write(program); err != nil {
		file.Close()

		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		return err
	}

	return os.Rename(temporary, path)
}

// Executable is the file of the running program, with links followed.
func Executable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(path)
}

// Arch is the processor of this computer, as the releases name it.
func Arch() string { return runtime.GOARCH }
