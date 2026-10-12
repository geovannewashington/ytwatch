package locale

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"": "", " PT ": "pt", "pt-br": "pt-BR", "PT-BR": "pt-BR", "zh-hans": "zh-Hans"} {
		if got, err := NormalizeLanguage(in); err != nil || got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"p", "portuguese", "pt_BR", "1a"} {
		if _, err := NormalizeLanguage(bad); err == nil {
			t.Errorf("NormalizeLanguage(%q) should fail", bad)
		}
	}
	for in, want := range map[string]string{"": "", "br": "BR", " jp ": "JP"} {
		if got, err := NormalizeRegion(in); err != nil || got != want {
			t.Errorf("NormalizeRegion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"B", "BRA", "B1", "br-"} {
		if _, err := NormalizeRegion(bad); err == nil {
			t.Errorf("NormalizeRegion(%q) should fail", bad)
		}
	}
}

func TestArgsAndParams(t *testing.T) {
	if got := (Locale{}).YtDlpArgs(); len(got) != 0 {
		t.Fatalf("no locale means no arguments: %v", got)
	}
	l := Locale{Language: "pt-BR", Region: "BR"}
	if want := []string{"--extractor-args", "youtube:lang=pt-BR", "--xff", "BR"}; !reflect.DeepEqual(l.YtDlpArgs(), want) {
		t.Fatalf("args = %v", l.YtDlpArgs())
	}
	if l.PageParams() != "hl=pt-BR&gl=BR" || (Locale{Region: "JP"}).PageParams() != "gl=JP" || (Locale{}).PageParams() != "" {
		t.Fatalf("params: %q", l.PageParams())
	}
	if (Locale{}).AcceptLanguage() != "en-US,en;q=0.9" || l.AcceptLanguage() != "pt-BR,en;q=0.5" {
		t.Fatal("Accept-Language is wrong")
	}
}
