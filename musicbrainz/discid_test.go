package musicbrainz

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestDiscID(t *testing.T) {
	t.Parallel()

	tcs := []struct {
		toc    TOC
		discID string
	}{
		{
			// Nirvana - Nevermind, from https://musicbrainz.org/doc/Disc_ID_Calculation
			TOC{LeadOut: 267257, Offsets: []int{150, 22767, 41887, 58317, 72102, 91375, 104652, 115380, 132165, 143932, 159870, 174597}},
			"I5l9cCSFccLKFEKS.7wqSZAorPU-",
		},
		// from the libdiscid and python-discid test suites
		{
			TOC{LeadOut: 303602, Offsets: []int{150, 9700, 25887, 39297, 53795, 63735, 77517, 94877, 107270, 123552, 135522, 148422, 161197, 174790, 192022, 205545, 218010, 228700, 239590, 255470, 266932, 288750}},
			"xUp1F2NkfP8s8jaeFn_Av3jNEI4-",
		},
		{
			TOC{LeadOut: 258725, Offsets: []int{150, 17510, 33275, 45910, 57805, 78310, 94650, 109580, 132010, 149160, 165115, 177710, 203325, 215555, 235590}},
			"TqvKjMu7dMliSfmVEBtrL7sBSno-",
		},
		{
			TOC{LeadOut: 44942, Offsets: []int{150}},
			"ANJa4DGYN_ktpzOwvVPtcjwP7mE-",
		},
	}
	for _, tc := range tcs {
		assert.Equal(t, tc.discID, tc.toc.DiscID())
	}
}

func TestLookupDiscID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "no", r.URL.Query().Get("cdstubs"))
		switch r.URL.Path {
		case "/discid/known":
			_, _ = w.Write([]byte(`{"id":"known","releases":[{"id":"a"},{"id":"b"}]}`))
		case "/discid/broken":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := MBClient{BaseURL: srv.URL, HTTPClient: srv.Client(), Limiter: rate.NewLimiter(rate.Inf, 1)}

	releaseIDs, err := c.LookupDiscID(t.Context(), "known")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, releaseIDs)

	releaseIDs, err = c.LookupDiscID(t.Context(), "unknown")
	require.NoError(t, err)
	assert.Empty(t, releaseIDs)

	_, err = c.LookupDiscID(t.Context(), "broken")
	require.Error(t, err)
}
