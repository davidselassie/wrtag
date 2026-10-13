package wrtag

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/wrtag/musicbrainz"
	"go.senan.xyz/wrtag/ripfile"
	"golang.org/x/time/rate"
)

// ⚠️ Note, core wrtag functionality is tested from ./cmd/wrtag/

func TestDiffer(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	diff(1, "x", "aaaaa", "aaaaa")
	diff(1, "x", "aaaaa", "aaaaX")
	assert.InEpsilon(t, 90.0, score, 0) // 9 of 10 chars the same
}

func TestDiffWeightsLowerBound(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	// all the same, but label/catalogue num mismatch
	diff(0, "label", "Columbia", "uh some other label")
	diff(0, "catalogue num", "Columbia", "not the same catalogue num")

	diff(1, "track 1", "The Day I Met God", "The Day I Met God")
	diff(1, "track 2", "Catholic Day", "Catholic Day")
	diff(1, "track 3", "Nine Plan Failed", "Nine Plan Failed")
	diff(1, "track 4", "Family of Noise", "Family of Noise")
	diff(1, "track 5", "Digital Tenderness", "Digital Tenderness")

	// but that's fine since we gave those 0 weight
	assert.InEpsilon(t, 100.0, score, 0)
}

func TestDiffWeightsUpperBound(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	// all the same, but label/catalogue num mismatch
	diff(2, "label", "Columbia", "uh some other label")
	diff(2, "catalogue num", "Columbia", "not the same catalogue num")

	diff(1, "track 1", "The Day I Met God", "The Day I Met God")
	diff(1, "track 2", "Catholic Day", "Catholic Day")
	diff(1, "track 3", "Nine Plan Failed", "Nine Plan Failed")
	diff(1, "track 4", "Family of Noise", "Family of Noise")
	diff(1, "track 5", "Digital Tenderness", "Digital Tenderness")

	// bad score since we really care about label / catalogue num
	assert.InDelta(t, 32.0, score, 1)
}

func TestDiffNorm(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	diff(1, "label", "Columbia", "COLUMBIA")
	diff(1, "catalogue num", "CLO LP 3", "CLOLP3")

	require.InEpsilon(t, 100.0, score, 0) // we don't care about case or spaces
}

func TestDiffIgnoreMissing(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	diff(1, "label", "", "COLUMBIA")
	diff(1, "catalogue num", "CLO LP 3", "CLOLP3")

	assert.InEpsilon(t, 100.0, score, 0)
}

// https://github.com/sentriz/wrtag/issues/99
func TestNegativeScore(t *testing.T) {
	t.Parallel()

	var score float64
	diff := Differ(&score)

	diff(1, "release", "Moon Boots", "Moon Boots")
	diff(1, "artist", "Bird Bear Hare and Fish", "BBHF")
	diff(1, "label", "", "SME Records")
	diff(1, "catalogue num", "", "SECL-2324")
	diff(1, "upc", "", "4547366368383")
	diff(1, "media format", "", "CD")

	diff(1, "track 1", "Bird Bear Hare and Fish – ウクライナ", "BBHF – ウクライナ")
	diff(1, "track 2", "Bird Bear Hare and Fish – ライカ", "BBHF – ライカ")
	diff(1, "track 3", "Bird Bear Hare and Fish – ダッシュボード", "BBHF – ダッシュボード")
	diff(1, "track 4", "Bird Bear Hare and Fish – レプリカント", "BBHF – レプリカント")
	diff(1, "track 5", "Bird Bear Hare and Fish – Hearts", "BBHF – Hearts")
	diff(1, "track 6", "Bird Bear Hare and Fish – 夏の光", "BBHF – 夏の光")
	diff(1, "track 7", "Bird Bear Hare and Fish – ページ", "BBHF – ページ")
	diff(1, "track 8", "Bird Bear Hare and Fish – Wake Up", "BBHF – Wake Up")
	diff(1, "track 9", "Bird Bear Hare and Fish – Different", "BBHF – Different")
	diff(1, "track 10", "Bird Bear Hare and Fish – 骨の音", "BBHF – 骨の音")
	diff(1, "track 11", "Bird Bear Hare and Fish – 次の火", "BBHF – 次の火")
	diff(1, "track 12", "Bird Bear Hare and Fish – Work", "BBHF – Work")

	assert.InEpsilon(t, 37, score, 1)
}

func TestDiffNormText(t *testing.T) {
	t.Parallel()

	assert.Empty(t, diffNormText(""))
	assert.Empty(t, diffNormText(" "))
	assert.Equal(t, "123", diffNormText(" 1!2!3 "))
	assert.Equal(t, "séan", diffNormText("SÉan"))
	assert.Equal(t, "hello世界", diffNormText("~~ 【 Hello, 世界。 】~~ 😉"))
}

func TestIsNonFatalError(t *testing.T) {
	t.Parallel()

	assert.True(t, IsNonFatalError(ErrScoreTooLow))
	assert.True(t, IsNonFatalError(ErrTrackCountMismatch))
	assert.False(t, IsNonFatalError(ErrNoTracks))
	assert.False(t, IsNonFatalError(ErrNotSortable))
	assert.False(t, IsNonFatalError(ErrSelfCopy))
}

func TestNewDirContext(t *testing.T) {
	t.Parallel()

	dc := NewDirContext()
	assert.NotNil(t, dc.knownDestPaths)
	assert.Empty(t, dc.knownDestPaths)
}

func TestMoveCanModifyDest(t *testing.T) {
	t.Parallel()

	move := NewMove(false)
	assert.True(t, move.CanModifyDest())

	dryRunMove := NewMove(true)
	assert.False(t, dryRunMove.CanModifyDest())
}

func TestCopyCanModifyDest(t *testing.T) {
	t.Parallel()

	cpy := NewCopy(false)
	assert.True(t, cpy.CanModifyDest())

	dryRunCopy := NewCopy(true)
	assert.False(t, dryRunCopy.CanModifyDest())
}

func TestReflinkCanModifyDest(t *testing.T) {
	t.Parallel()

	reflink := NewReflink(false)
	assert.True(t, reflink.CanModifyDest())

	dryRunReflink := NewReflink(true)
	assert.False(t, dryRunReflink.CanModifyDest())
}

func TestIntersect(t *testing.T) {
	t.Parallel()

	assert.Nil(t, intersect(nil))
	assert.Equal(t, []string{"a", "b"}, intersect([][]string{{"a", "b", "a"}}))
	assert.Equal(t, []string{"b", "c"}, intersect([][]string{{"a", "b", "c"}, {"c", "b"}, {"d", "b", "c"}}))
	assert.Empty(t, intersect([][]string{{"a"}, {"b"}}))
}

func TestExtendQueryWithRipDiscs(t *testing.T) {
	t.Parallel()

	// disc 1 and 2 are both on releases a and b, and c only has disc 1. other is on a different release.
	// looking up broken is an error, and unknown isn't on any releases
	responses := map[string]string{
		"disc1": `{"releases":[{"id":"a"},{"id":"b"},{"id":"c"}]}`,
		"disc2": `{"releases":[{"id":"b"},{"id":"a"}]}`,
		"other": `{"releases":[{"id":"o"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discID := strings.TrimPrefix(r.URL.Path, "/discid/")
		if discID == "broken" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp, ok := responses[discID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)

	mb := &musicbrainz.MBClient{BaseURL: srv.URL, HTTPClient: srv.Client(), Limiter: rate.NewLimiter(rate.Inf, 1)}

	// each disc has 2 tracks
	discs := func(ids ...string) []ripfile.Disc {
		var discs []ripfile.Disc
		for _, id := range ids {
			discs = append(discs, ripfile.Disc{ID: id, NumTracks: 2})
		}
		return discs
	}

	tcs := []struct {
		name      string
		discs     []ripfile.Disc
		numTracks int
		format    string
		catNum    string
		barcode   string
		expFilter []string
		expFormat string
	}{
		{name: "no discs", numTracks: 4},
		{name: "single disc", discs: discs("disc1"), numTracks: 2, expFilter: []string{"a", "b", "c"}, expFormat: "CD"},
		{name: "multi disc", discs: discs("disc1", "disc2"), numTracks: 4, expFilter: []string{"a", "b"}, expFormat: "CD"},
		{name: "unknown disc is skipped", discs: discs("disc1", "unknown"), numTracks: 4, expFilter: []string{"a", "b", "c"}, expFormat: "CD"},
		{name: "lookup error is skipped", discs: discs("disc1", "broken"), numTracks: 4, expFilter: []string{"a", "b", "c"}, expFormat: "CD"},
		{name: "different number of tracks", discs: discs("disc1"), numTracks: 3},
		{name: "no common release", discs: discs("disc1", "other"), numTracks: 4, expFormat: "CD"},
		{name: "tags have a format", discs: discs("disc1"), numTracks: 2, format: "Enhanced CD", expFilter: []string{"a", "b", "c"}, expFormat: "Enhanced CD"},
		{name: "catalogue number is known", discs: discs("disc1"), numTracks: 2, catNum: "DGCD-24425", expFormat: "CD"},
		{name: "barcode is known", discs: discs("disc1"), numTracks: 2, barcode: "720642442524", expFormat: "CD"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q := musicbrainz.ReleaseQuery{NumTracks: tc.numTracks, Format: tc.format, CatalogueNum: tc.catNum, Barcode: tc.barcode}
			require.NoError(t, extendQueryWithRipDiscs(t.Context(), mb, &q, tc.discs))
			assert.Equal(t, tc.expFilter, q.FilterMBReleaseIDs)
			assert.Equal(t, tc.expFormat, q.Format)
		})
	}
}
