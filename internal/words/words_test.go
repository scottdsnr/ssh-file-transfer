package words

import (
	"strings"
	"testing"
)

func TestGenerateHasFourParts(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code := Generate()
		if got := len(strings.Split(code, "-")); got != 4 {
			t.Fatalf("code %q has %d parts, want 4", code, got)
		}
		seen[code] = true
	}
	if len(seen) < 45 {
		t.Fatalf("only %d distinct codes out of 50; randomness looks weak", len(seen))
	}
}

func TestRoom(t *testing.T) {
	room, err := Room(" 1234-cobalt-badger-orbit ")
	if err != nil {
		t.Fatal(err)
	}
	if room != "1234" {
		t.Fatalf("room = %q, want 1234", room)
	}
	if _, err := Room("nodashes"); err == nil {
		t.Fatal("expected an error for a code with no dash")
	}
}
