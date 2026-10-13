package ripfile

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"

	"go.senan.xyz/wrtag/musicbrainz"
)

func parseLog(text string) []Disc {
	switch {
	case strings.HasPrefix(text, "Log created by: whipper"):
		return loggedDiscs(text, whipperDiscIDExpr, whipperNumTracksExpr)
	case strings.HasPrefix(text, "cyanrip "):
		return loggedDiscs(text, cyanripDiscIDExpr, cyanripNumTracksExpr)
	case strings.HasPrefix(text, "dBpoweramp "):
		return tocDiscs(parseDBpowerampLog(text))
	default:
		// EAC, XLD, CUERipper, fre:ac. these all have a TOC table with the same columns
		return tocDiscs(parseTOCTables(text))
	}
}

// trackSectors is a track as reported in a log. Start and End are both inclusive, and are 0 based
// LBAs, so they don't include [musicbrainz.PregapSectors].
type trackSectors struct {
	Num, Start, End int
}

// tocDiscs returns the discs for the rips in a log, where each rip is the tracks of a whole disc.
func tocDiscs(rips [][]trackSectors) []Disc {
	var discs []Disc
	for _, tracks := range rips {
		toc, ok := buildTOC(tracks)
		if !ok {
			continue // probably a partial rip, or not a TOC at all
		}
		discs = append(discs, Disc{ID: toc.DiscID(), NumTracks: len(toc.Offsets)})
	}
	return discs
}

// dataTrackGap is the gap between the audio session and a trailing data session on an Enhanced
// CD, which is the lead-out, lead-in, and pregap of the second session.
const dataTrackGap = 11400

// buildTOC converts the tracks from a log to a MusicBrainz TOC.
func buildTOC(tracks []trackSectors) (musicbrainz.TOC, bool) {
	// an enhanced CD has a data track after a gap at the end, which isn't part of the audio session
	if n := len(tracks); n > 1 && tracks[n-1].Start-(tracks[n-2].End+1) == dataTrackGap {
		tracks = tracks[:n-1]
	}
	if len(tracks) == 0 || len(tracks) > 99 {
		return musicbrainz.TOC{}, false
	}

	var toc musicbrainz.TOC
	for i, t := range tracks {
		// tracks on a disc are numbered from 1, and each starts right where the last one ended. this also
		// rejects most partial rips, mixed mode CDs with a data track first, and tables that aren't TOCs at all
		if t.Num != i+1 || t.End < t.Start || (i > 0 && t.Start != tracks[i-1].End+1) {
			return musicbrainz.TOC{}, false
		}
		toc.Offsets = append(toc.Offsets, t.Start+musicbrainz.PregapSectors)
	}
	toc.LeadOut = tracks[len(tracks)-1].End + 1 + musicbrainz.PregapSectors
	return toc, true
}

// tocTableRowExpr matches a row of the TOC table used by EAC, XLD, CUERipper, and fre:ac. We match by the
// shape of the row since the header is translated in localised versions of EAC. For example
//
//	Track |   Start  |  Length  | Start sector | End sector
//	---------------------------------------------------------
//	   1  |  0:00.00 |  5:32.14 |         0    |    24913
//
// The times are m:ss.ff in EAC and mm:ss:ff in XLD. fre:ac prefixes each line with a timestamp.
var tocTableRowExpr = regexp.MustCompile(`(\d+)\s*\|\s*[\d:.]+\s*\|\s*[\d:.]+\s*\|\s*(\d+)\s*\|\s*(\d+)\s*$`)

// parseTOCTables returns the tracks from each TOC table in the log. A table is any run of consecutive rows.
func parseTOCTables(text string) [][]trackSectors {
	var tables [][]trackSectors
	var table []trackSectors

	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		m := tocTableRowExpr.FindStringSubmatch(sc.Text())
		if m == nil {
			if len(table) > 0 {
				tables = append(tables, table)
				table = nil
			}
			continue
		}
		table = append(table, trackSectors{Num: atoi(m[1]), Start: atoi(m[2]), End: atoi(m[3])})
	}
	if len(table) > 0 {
		tables = append(tables, table)
	}
	return tables
}

// dBpoweramp only logs the ripped tracks, and the end LBA is exclusive. For example
//
//	Track 1:  Ripped LBA 0 to 24914 (5:32) in 0:30. Filename: ...
var dBpowerampTrackExpr = regexp.MustCompile(`^Track (\d+):\s+Ripped LBA (\d+) to (\d+)`)

func parseDBpowerampLog(text string) [][]trackSectors {
	var rips [][]trackSectors
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		m := dBpowerampTrackExpr.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		t := trackSectors{Num: atoi(m[1]), Start: atoi(m[2]), End: atoi(m[3]) - 1}
		if t.Num == 1 || len(rips) == 0 {
			rips = append(rips, nil) // a new rip, if logs are concatenated
		}
		rips[len(rips)-1] = append(rips[len(rips)-1], t)
	}
	return rips
}

// whipper and cyanrip log the MusicBrainz disc ID and number of tracks of the disc. For example from whipper
//
//	MusicBrainz Disc ID: C2svSeTdtJVtN7VQR__XwmEScU8-
//	MusicBrainz lookup URL: https://musicbrainz.org/cdtoc/attach?toc=1+8+149323+150+...&tracks=8&id=...
//
// And from cyanrip
//
//	Disc tracks: 2
//	DiscID: 7rGk5fXmuE8VcWYpbwoobOZIcIE-
var (
	whipperDiscIDExpr    = regexp.MustCompile(`^\s*MusicBrainz Disc ID:\s*([A-Za-z0-9._]{27}-)\s*$`)
	whipperNumTracksExpr = regexp.MustCompile(`^\s*MusicBrainz lookup URL:.*[?&]tracks=(\d+)`)
	cyanripDiscIDExpr    = regexp.MustCompile(`^DiscID:\s*([A-Za-z0-9._]{27}-)\s*$`)
	cyanripNumTracksExpr = regexp.MustCompile(`^Disc tracks:\s*(\d+)`)
)

// loggedDiscs returns the discs from a log which has the disc ID and number of tracks of each disc, in either order.
func loggedDiscs(text string, discIDExpr, numTracksExpr *regexp.Regexp) []Disc {
	var discs []Disc
	var disc Disc

	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		if m := discIDExpr.FindStringSubmatch(line); m != nil {
			disc.ID = m[1]
		}
		if m := numTracksExpr.FindStringSubmatch(line); m != nil {
			disc.NumTracks = atoi(m[1])
		}
		if disc.ID != "" && disc.NumTracks > 0 {
			discs = append(discs, disc)
			disc = Disc{}
		}
	}
	return discs
}

func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}
