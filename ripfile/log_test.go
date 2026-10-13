package ripfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/wrtag/musicbrainz"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

// pornophonique - Brave New World, which is on https://musicbrainz.org/cdtoc/C2svSeTdtJVtN7VQR__XwmEScU8-
var braveNewWorld = Disc{ID: "C2svSeTdtJVtN7VQR__XwmEScU8-", NumTracks: 8}

const eacLog = `Exact Audio Copy V1.6 from 23. October 2020

TOC of the extracted CD

     Track |   Start  |  Length  | Start sector | End sector
    ---------------------------------------------------------
        1  |  0:00.00 |  5:32.14 |         0    |    24913
        2  |  5:32.14 |  4:07.22 |     24914    |    43460
        3  |  9:39.36 |  3:50.29 |     43461    |    60739
        4  | 13:29.65 |  4:56.00 |     60740    |    82939
        5  | 18:25.65 |  3:45.35 |     82940    |    99849
        6  | 22:11.25 |  3:20.57 |     99850    |   114906
        7  | 25:32.07 |  4:33.26 |    114907    |   135407
        8  | 30:05.33 |  3:03.40 |    135408    |   149172

Track  1
`

// eacLogGerman has translated headers, but the table has the same shape.
var eacLogGerman = strings.NewReplacer(
	"from 23. October", "vom 23. Oktober",
	"TOC of the extracted CD", "TOC der ausgelesenen CD",
	"Track |", "Titel |", "Length", "Länge", "Start sector", "Startsektor", "End sector", "Endsektor",
).Replace(eacLog)

const dBpowerampLog = `dBpoweramp 2022-09-02 Digital Audio Extraction Log from Thursday, 22. September 2022 08:03

Track 1:  Ripped LBA 0 to 24914 (5:32) in 0:30. Filename: C:\Music\01 Coming Home.m4a
Track 2:  Ripped LBA 24914 to 43461 (4:07) in 0:20. Filename: C:\Music\02 Save Game.m4a
Track 3:  Ripped LBA 43461 to 60740 (3:50) in 0:17. Filename: C:\Music\03 Voices in My Head.m4a
Track 4:  Ripped LBA 60740 to 82940 (4:56) in 0:20. Filename: C:\Music\04 The Songs We Sang Together.m4a
Track 5:  Ripped LBA 82940 to 99850 (3:45) in 0:14. Filename: C:\Music\05 Night Will Fall.m4a
Track 6:  Ripped LBA 99850 to 114907 (3:20) in 0:12. Filename: C:\Music\06 Brave New World.m4a
Track 7:  Ripped LBA 114907 to 135408 (4:33) in 0:16. Filename: C:\Music\07 Wave After Wave.m4a
Track 8:  Ripped LBA 135408 to 149173 (3:03) in 0:10. Filename: C:\Music\08 Awakening.m4a
`

const otherDiscLog = `1 | 0:00.00 | 1:00.00 | 0    | 4499
2 | 1:00.00 | 1:00.00 | 4500 | 8999
`

var otherDisc = Disc{ID: musicbrainz.TOC{LeadOut: 9150, Offsets: []int{150, 4650}}.DiscID(), NumTracks: 2}

const whipperLog = `Log created by: whipper 0.10.0 (internal logger)

CD metadata:
  Release:
    Artist: pornophonique
    Title: Brave New World
  MusicBrainz Disc ID: C2svSeTdtJVtN7VQR__XwmEScU8-
  MusicBrainz lookup URL: https://musicbrainz.org/cdtoc/attach?toc=1+8+149323+150+25064+43611+60890+83090+100000+115057+135558&tracks=8&id=C2svSeTdtJVtN7VQR__XwmEScU8-
`

func TestParseLog(t *testing.T) {
	t.Parallel()

	tcs := []struct {
		name string
		log  string
		exp  []Disc
	}{
		{"eac", eacLog, []Disc{braveNewWorld}},
		{"eac german", eacLogGerman, []Disc{braveNewWorld}},
		{"cueripper", "CUERipper v2.1.6 Copyright (C) 2008-2021 Grigory Chudov\n" + eacLog, []Disc{braveNewWorld}},
		{"xld", "X Lossless Decoder version 20211018 (154.0)\n" + strings.ReplaceAll(eacLog, ".", ":"), []Disc{braveNewWorld}},
		{"freac", freacLog(eacLog), []Disc{braveNewWorld}},
		{"dbpoweramp", dBpowerampLog, []Disc{braveNewWorld}},
		{"whipper", whipperLog, []Disc{braveNewWorld}},
		{"cyanrip", "cyanrip 0.9.3 (c23f1c4)\nDisc tracks: 2\nDiscID: 7rGk5fXmuE8VcWYpbwoobOZIcIE-\r\n", []Disc{{ID: "7rGk5fXmuE8VcWYpbwoobOZIcIE-", NumTracks: 2}}},

		{"concatenated", eacLog + otherDiscLog, []Disc{braveNewWorld, otherDisc}},
		{"dbpoweramp concatenated", dBpowerampLog + dBpowerampLog, []Disc{braveNewWorld, braveNewWorld}},
		{
			// an enhanced CD, where track 3 is a data track in a second session
			"data track",
			otherDiscLog + "3 | 2:00.00 | 1:06.60 | 20400 | 25409\n",
			[]Disc{otherDisc},
		},
		{
			// looks like a TOC table, but track 2 doesn't start where track 1 ends
			"not contiguous",
			"1 | 0:00 | 3:21 | 0 | 15000\n2 | 3:21 | 4:00 | 15100 | 33000\n",
			nil,
		},
		{
			// track 3 wasn't ripped, so we don't know the whole TOC
			"dbpoweramp missing track",
			strings.Replace(dBpowerampLog, "Track 3:", "", 1),
			nil,
		},
		{"whipper no track count", strings.Replace(whipperLog, "&tracks=8", "", 1), nil},
		{"cyanrip no track count", "cyanrip 0.9.3 (c23f1c4)\nDiscID: 7rGk5fXmuE8VcWYpbwoobOZIcIE-\n", nil},
		{"empty", "", nil},
		{"not a log", "some notes about this rip\n| a | b | c | d |\n", nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.exp, parseLog(tc.log))
		})
	}
}

func TestParseLogEncodings(t *testing.T) {
	t.Parallel()

	utf16, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(eacLogGerman)
	require.NoError(t, err)
	cp1252, err := charmap.Windows1252.NewEncoder().String(eacLogGerman)
	require.NoError(t, err)

	tcs := map[string]string{
		"utf-8":          eacLogGerman,
		"utf-8 with bom": "\uFEFF" + eacLogGerman,
		"utf-16le":       utf16,
		"windows-1252":   cp1252,
	}
	for name, b := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "rip.log")
			require.NoError(t, os.WriteFile(path, []byte(b), 0o600))

			discs, err := readLog(path)
			require.NoError(t, err)
			assert.Equal(t, []Disc{braveNewWorld}, discs)
		})
	}
}

// freacLog formats a log's TOC table how fre:ac does, with a timestamp before each line, and a data track.
func freacLog(log string) string {
	var sb strings.Builder
	sb.WriteString("Conversion #1 - 8 tracks\n\nClient:  fre:ac - free audio converter\n\n")
	for line := range strings.Lines(log) {
		sb.WriteString("00:00:00.081         " + line)
	}
	sb.WriteString("00:00:00.082          DATA |          |          |       160723 |     170000\n")
	return sb.String()
}
