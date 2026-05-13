// Package export provides functions to export capture data to common file
// formats: CSV, Excel (.xlsx), SQLite, Parquet, and WAV.
//
// Each export function reads all remaining frames from a [capture.Reader]
// and writes them to the target format. The reader is consumed by the call;
// create a new reader for each export.
package export

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/capture"
)

// columnNames returns a display name for each channel. Named channels use
// their name; unnamed channels get "ch0", "ch1", etc. Duplicates are
// suffixed with _2, _3, ... to guarantee uniqueness.
func columnNames(channels []capture.Channel) []string {
	names := make([]string, len(channels))
	used := make(map[string]bool)
	for i, ch := range channels {
		name := ch.Name
		if name == "" {
			name = fmt.Sprintf("ch%d", i)
		}
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s_%d", base, n)
		}
		used[name] = true
		names[i] = name
	}
	return names
}
