package download

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsURL(t *testing.T) {
	t.Parallel()

	assert.True(t, IsURL("http://example.com/music/"))
	assert.True(t, IsURL("https://example.com/a.zip"))
	assert.False(t, IsURL("/music/http://"))
	assert.False(t, IsURL("ftp://example.com/a.zip"))
	assert.False(t, IsURL("http:///no-host"))
	assert.False(t, IsURL("relative/dir"))
}

func TestListingLinks(t *testing.T) {
	t.Parallel()

	// like Apache's mod_autoindex, with its parent and sort links, and some odd ones
	const listing = `<html><body><table>
<tr><th><a href="?C=N;O=D">Name</a></th><th><a href="?C=M;O=A">Last modified</a></th></tr>
<tr><td><a href="/music/">Parent Directory</a></td></tr>
<tr><td><a href="../">Up</a></td></tr>
<tr><td><a href="01%20Alarms.flac">01 Alarms.flac</a></td></tr>
<tr><td><a href="01%20Alarms.flac">01 Alarms.flac again</a></td></tr>
<tr><td><a href="/music/Album/cover.jpg#top">cover.jpg</a></td></tr>
<tr><td><a href="disc%201/">disc 1/</a></td></tr>
<tr><td><a href="disc%201/02.flac">nested</a></td></tr>
<tr><td><a href=".//">self, doubled slash</a></td></tr>
</table></body></html>`

	base, err := url.Parse("https://example.com/music/Album/")
	require.NoError(t, err)

	links, err := listingLinks(base, strings.NewReader(listing))
	require.NoError(t, err)

	var got []string
	for _, l := range links {
		got = append(got, l.String())
	}
	assert.Equal(t, []string{
		"https://example.com/music/Album/01%20Alarms.flac",
		"https://example.com/music/Album/cover.jpg",
		"https://example.com/music/Album/disc%201/",
	}, got)
}

func TestDownloadListing(t *testing.T) {
	t.Parallel()

	srv := t.TempDir()
	for _, p := range []string{"Album/01.flac", "Album/cover.jpg", "Album/disc 2/01.flac", "Album/disc 2/deeper/x.flac", "elsewhere.flac"} {
		require.NoError(t, os.MkdirAll(filepath.Join(srv, filepath.Dir(p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(srv, p), []byte(p), 0o600))
	}
	ts := httptest.NewServer(http.FileServer(http.Dir(srv)))
	t.Cleanup(ts.Close)

	dir := t.TempDir()
	archive, err := fetch(context.Background(), ts.URL+"/Album", dir)
	require.NoError(t, err)
	assert.Empty(t, archive)

	var got []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	}))
	assert.ElementsMatch(t, []string{"01.flac", "cover.jpg", "disc 2/01.flac"}, got)

	data, err := os.ReadFile(filepath.Join(dir, "disc 2", "01.flac"))
	require.NoError(t, err)
	assert.Equal(t, "Album/disc 2/01.flac", string(data))
}

func TestDownloadZipBySignature(t *testing.T) {
	t.Parallel()

	srv := t.TempDir()
	{
		f, err := os.Create(filepath.Join(srv, "archive.zip"))
		require.NoError(t, err)
		zw := zip.NewWriter(f)
		_, err = zw.Create("Album/01.flac")
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, f.Close())
	}

	// a download link with no zip name or content type
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, filepath.Join(srv, "archive.zip"))
	}))
	t.Cleanup(ts.Close)

	dir, cleanup, err := Release(context.Background(), ts.URL+"/download.php?id=1")
	require.NoError(t, err)
	assert.Equal(t, "Album", filepath.Base(dir))
	assert.FileExists(t, filepath.Join(dir, "01.flac"))

	cleanup()
	assert.NoDirExists(t, dir)
}

func TestFileName(t *testing.T) {
	t.Parallel()

	fileNameOf := func(rawURL, disposition string) (string, error) {
		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		resp := &http.Response{Request: &http.Request{URL: u}, Header: http.Header{}, Body: http.NoBody}
		defer resp.Body.Close()
		if disposition != "" {
			resp.Header.Set("Content-Disposition", disposition)
		}
		return fileName(resp)
	}

	name, err := fileNameOf("https://example.com/dl/Some%20Album.flac", "")
	require.NoError(t, err)
	assert.Equal(t, "Some Album.flac", name)

	name, err = fileNameOf("https://example.com/download?id=1", `attachment; filename="01 Track.flac"`)
	require.NoError(t, err)
	assert.Equal(t, "01 Track.flac", name)

	name, err = fileNameOf("https://example.com/download?id=1", `attachment; filename*=UTF-8''02%20Tr%C3%A4ck.flac`)
	require.NoError(t, err)
	assert.Equal(t, "02 Träck.flac", name)

	name, err = fileNameOf("https://example.com/download?id=1", `attachment; filename="..\\..\\evil.flac"`)
	require.NoError(t, err)
	assert.Equal(t, "evil.flac", name)

	_, err = fileNameOf("https://example.com/", "")
	require.Error(t, err)
}

func TestDownloadListingUsesServerFileNames(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/Album/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="get?id=1">01</a> <a href="get?id=2">02</a>`))
	})
	mux.HandleFunc("/Album/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="0`+r.URL.Query().Get("id")+` Track.flac"`)
		_, _ = w.Write([]byte("audio"))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	dir, cleanup, err := Release(context.Background(), ts.URL+"/Album/")
	require.NoError(t, err)
	t.Cleanup(cleanup)
	assert.FileExists(t, filepath.Join(dir, "01 Track.flac"))
	assert.FileExists(t, filepath.Join(dir, "02 Track.flac"))
}
