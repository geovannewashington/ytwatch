package theme

import (
	"slices"
	"testing"
)

func TestEveryPresetDefinesEveryRoleWithValidColours(t *testing.T) {
	for _, name := range Names() {
		preset, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) failed", name)
		}
		for _, role := range Roles {
			value, ok := preset.Colors[role]
			if !ok || !ValidColor(value) {
				t.Errorf("%s: role %q is missing or invalid: %q", name, role, value)
			}
		}
		if len(preset.Colors) != len(Roles) {
			t.Errorf("%s defines %d colours, want %d", name, len(preset.Colors), len(Roles))
		}
	}
	if !slices.Contains(Names(), Default) {
		t.Fatalf("default theme %q is not a preset", Default)
	}
}

func TestResolve(t *testing.T) {
	got, err := Resolve("", nil)
	if err != nil || got.Name != Default {
		t.Fatalf("empty name = %+v, %v", got, err)
	}
	got, err = Resolve("Nord", map[string]string{"accent": "#112233", "live": "9"})
	if err != nil || got.Color("accent") != "#112233" || got.Color("live") != "9" || got.Color("secondary") != "#88c0d0" {
		t.Fatalf("overrides not applied: %+v, %v", got, err)
	}
	for _, bad := range []struct {
		name      string
		overrides map[string]string
	}{
		{"nope", nil},
		{"nord", map[string]string{"accent": "green"}},
		{"nord", map[string]string{"accent": "#12345"}},
		{"nord", map[string]string{"accent": "256"}},
		{"nord", map[string]string{"bogus": "#112233"}},
	} {
		if _, err := Resolve(bad.name, bad.overrides); err == nil {
			t.Errorf("Resolve(%q, %v) should fail", bad.name, bad.overrides)
		}
	}
}

func TestLookupReturnsACopy(t *testing.T) {
	a, _ := Lookup("nord")
	a.Colors["accent"] = "#000000"
	if b, _ := Lookup("nord"); b.Color("accent") == "#000000" {
		t.Fatal("changing a looked-up theme must not change the preset")
	}
}

func TestNextWrapsAround(t *testing.T) {
	names := Names()
	if got := Next(names[len(names)-1]); got.Name != names[0] {
		t.Fatalf("after the last preset got %q, want %q", got.Name, names[0])
	}
	if got := Next("unknown"); got.Name != names[0] {
		t.Fatalf("after an unknown name got %q, want %q", got.Name, names[0])
	}
	if got := Next(names[0]); got.Name != names[1] {
		t.Fatalf("after %q got %q, want %q", names[0], got.Name, names[1])
	}
}
