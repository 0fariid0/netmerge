package state

import "testing"

func TestDistributionPersistsAndRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	if mode, err := LoadDistribution(dir); err != nil || mode != "" {
		t.Fatalf("missing preference = %q, %v", mode, err)
	}
	for _, mode := range []string{"fast-download", "game", "round-robin", "smart"} {
		if err := SaveDistribution(dir, mode); err != nil {
			t.Fatal(err)
		}
		if got, err := LoadDistribution(dir); err != nil || got != mode {
			t.Fatalf("saved mode = %q, %v; want %q", got, err, mode)
		}
	}
	if err := SaveDistribution(dir, "invalid"); err == nil {
		t.Fatal("invalid value was accepted")
	}
	if got, err := LoadDistribution(dir); err != nil || got != "smart" {
		t.Fatalf("last valid mode was lost: %q, %v", got, err)
	}
}
