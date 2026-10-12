package preview

import "testing"

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
