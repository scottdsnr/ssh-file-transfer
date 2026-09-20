// Package words generates and parses the human-speakable transfer codes,
// which look like "1234-cobalt-badger-orbit".
package words

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// list is deliberately short, unambiguous and easy to read aloud.
var list = []string{
	"amber", "anchor", "arcade", "aspen", "badger", "bamboo", "beacon", "bison",
	"bonsai", "boulder", "cactus", "canyon", "cedar", "cobalt", "comet", "copper",
	"coral", "cosmos", "crater", "cricket", "cypress", "dahlia", "delta", "dune",
	"ember", "falcon", "fern", "fjord", "flint", "galaxy", "garnet", "geyser",
	"ginger", "glacier", "granite", "harbor", "hazel", "heron", "indigo", "ivory",
	"jasper", "juniper", "kelp", "kestrel", "lagoon", "lantern", "lichen", "lilac",
	"lumen", "lynx", "magnet", "maple", "marble", "meadow", "mesa", "meteor",
	"mirage", "monsoon", "nectar", "nimbus", "oasis", "obsidian", "ochre", "onyx",
	"opal", "orbit", "orchid", "osprey", "otter", "pebble", "pelican", "pewter",
	"pine", "plasma", "prairie", "quartz", "quasar", "quill", "raven", "reef",
	"ripple", "saffron", "sage", "salmon", "sequoia", "shale", "sierra", "slate",
	"sonnet", "spruce", "summit", "tamarind", "thicket", "thistle", "tundra",
	"umber", "valley", "vertex", "violet", "walnut", "willow", "zenith", "zephyr",
}

// Generate returns a fresh random code.
func Generate() string {
	parts := []string{fmt.Sprintf("%04d", randInt(10000))}
	for i := 0; i < 3; i++ {
		parts = append(parts, list[randInt(int64(len(list)))])
	}
	return strings.Join(parts, "-")
}

func randInt(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic("words: system randomness is unavailable: " + err.Error())
	}
	return v.Int64()
}

// Room is the part of the code the relay uses to pair two peers. It is only a
// rendezvous label; the rest of the code stays secret and feeds the PAKE, so
// the relay never learns enough to decrypt anything.
func Room(code string) (string, error) {
	room, _, ok := strings.Cut(strings.TrimSpace(code), "-")
	if !ok || room == "" {
		return "", fmt.Errorf("words: %q is not a valid code (expected something like 1234-cobalt-badger-orbit)", code)
	}
	return room, nil
}
