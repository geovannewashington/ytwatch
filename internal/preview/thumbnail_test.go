package preview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidFileID(t *testing.T) {
	cases := map[string]bool{
		"jNQXAC9IVRw":                        true,
		"PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI": true, // playlist id, 34 characters
		"short":                              false,
		"../../etc/passwd-is-not-valid":      false,
		"":                                   false,
	}
	for id, want := range cases {
		if got := validFileID(id); got != want {
			t.Errorf("validFileID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestFetchImageSkipsMissingAndWebP(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	webp := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 16)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jpeg":
			_, _ = w.Write(jpeg)
		case "/webp":
			_, _ = w.Write(webp)
		case "/missing":
			http.NotFound(w, r)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	if data, err := fetchImage(context.Background(), server.URL+"/jpeg"); err != nil || len(data) != len(jpeg) {
		t.Fatalf("jpeg: %v, %d bytes", err, len(data))
	}
	for _, path := range []string{"/webp", "/missing"} {
		if _, err := fetchImage(context.Background(), server.URL+path); !errors.Is(err, errUnusable) {
			t.Errorf("%s: err = %v, want errUnusable", path, err)
		}
	}
	if _, err := fetchImage(context.Background(), server.URL+"/error"); err == nil || errors.Is(err, errUnusable) {
		t.Errorf("a server error must fail, not fall through: %v", err)
	}
}
