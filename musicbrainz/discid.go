package musicbrainz

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 is part of the disc ID spec, not used for security
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PregapSectors is the length of the lead-in that precedes the first track of every CD.
// MusicBrainz TOC offsets are absolute, so they include it. 75 sectors per second, 2 seconds.
const PregapSectors = 150

// TOC is the table of contents of the audio session of a CD, starting at track 1. Offsets are the
// absolute sector addresses (LBA + [PregapSectors]) of each track, and LeadOut is the absolute sector
// address of the lead-out, which is where the audio session ends.
//
// https://musicbrainz.org/doc/Disc_ID_Calculation
type TOC struct {
	LeadOut int
	Offsets []int
}

// DiscID calculates the MusicBrainz disc ID for the TOC.
func (t TOC) DiscID() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%02X%02X%08X", 1, len(t.Offsets), t.LeadOut)
	for i := range 99 {
		var offset int
		if i < len(t.Offsets) {
			offset = t.Offsets[i]
		}
		fmt.Fprintf(&sb, "%08X", offset)
	}

	sum := sha1.Sum([]byte(sb.String())) //nolint:gosec // see import
	return discIDEncoding.EncodeToString(sum[:])
}

// discIDEncoding is base64 with "._-" in place of "+/=", so that disc IDs are safe in URLs.
var discIDEncoding = base64.NewEncoding(
	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._",
).WithPadding('-')

// LookupDiscID returns the IDs of the releases which have a disc with the disc ID, or none if MusicBrainz
// doesn't know it.
func (c *MBClient) LookupDiscID(ctx context.Context, discID string) ([]string, error) {
	urlV := url.Values{}
	urlV.Set("fmt", "json")
	urlV.Set("cdstubs", "no")

	url, _ := url.Parse(joinPath(c.BaseURL, "discid", discID))
	url.RawQuery = urlV.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)

	var resp struct {
		Releases []struct {
			ID string `json:"id"`
		} `json:"releases"`
	}
	if err := c.request(ctx, req, &resp); err != nil {
		if se := StatusError(0); errors.As(err, &se) && se == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("request discid: %w", err)
	}

	releaseIDs := make([]string, 0, len(resp.Releases))
	for _, r := range resp.Releases {
		releaseIDs = append(releaseIDs, r.ID)
	}
	return releaseIDs, nil
}
