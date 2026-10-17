package wstate

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadFixture reads a recorded-tree fixture file (the recorder's format:
// this package's Tree JSON) and validates its format version. Seam-2 tests
// feed fixtures to Render; a fixture captured by the recorder and one
// hand-authored in the same format are indistinguishable to the renderer.
func LoadFixture(path string) (*Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Tree
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	if t.Format != FixtureFormat {
		return nil, fmt.Errorf("%s: fixture format %d not supported (want %d)", path, t.Format, FixtureFormat)
	}
	return &t, nil
}
