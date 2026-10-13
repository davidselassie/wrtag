// Package ripfile reads the logs that CD rippers write alongside a rip, to find the discs that were ripped.
package ripfile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.senan.xyz/wrtag/fileutil"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// rip logs are small, so anything bigger than this is probably something else.
const maxFileSize = 4 << 20 // 4 MiB

// Disc is a CD that was ripped.
type Disc struct {
	// ID is the MusicBrainz disc ID.
	ID string
	// NumTracks is the number of audio tracks on the disc.
	NumTracks int
}

// Find returns the discs from the rip logs in dir, and in its direct subdirectories for multi-disc releases
// such as disc1/ and disc2/. Logs are files ending in .log or .txt, from EAC (in any language), XLD,
// CUERipper, fre:ac, dBpoweramp, whipper, or cyanrip. Logs from multiple rips can be concatenated. These
// files are only hints, so ones that can't be read are skipped.
func Find(ctx context.Context, dir string) ([]Disc, error) {
	mainPaths, err := fileutil.GlobDir(dir, "*")
	if err != nil {
		return nil, fmt.Errorf("glob dir: %w", err)
	}
	discPaths, err := fileutil.GlobDir(dir, "*/*")
	if err != nil {
		return nil, fmt.Errorf("glob dir for discs: %w", err)
	}

	var discs []Disc
	for _, path := range append(mainPaths, discPaths...) {
		if ext := strings.ToLower(filepath.Ext(path)); ext != ".log" && ext != ".txt" {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > maxFileSize {
			continue
		}

		logDiscs, err := readLog(path)
		if err != nil {
			slog.WarnContext(ctx, "skipping log", "path", path, "err", err)
			continue
		}
		if len(logDiscs) == 0 {
			continue
		}
		slog.DebugContext(ctx, "found rip log", "path", path, "discs", logDiscs)

		// the same disc can be ripped more than once
		for _, disc := range logDiscs {
			if !slices.Contains(discs, disc) {
				discs = append(discs, disc)
			}
		}
	}
	return discs, nil
}

func readLog(path string) ([]Disc, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is from the user's source dir
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	// EAC writes UTF-16 with a BOM, and most others UTF-8. older EAC versions write the system code page,
	// but we only need the ASCII parts of the log, so that's fine as it is
	text, _, err := transform.Bytes(unicode.BOMOverride(transform.Nop), b)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return parseLog(string(text)), nil
}
