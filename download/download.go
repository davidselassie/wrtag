// Package download fetches a release from an HTTP URL, so it can be imported like a directory.
package download

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"go.senan.xyz/wrtag/ziputil"
)

// IsURL reports whether src is an http or https URL.
func IsURL(src string) bool {
	u, err := url.Parse(src)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Release fetches the release at the URL src to a temporary directory. It returns the directory of the release, and
// a cleanup func which removes the temporary files.
//
// A URL serving an HTML page is read as a directory listing, like those from nginx, Apache or Caddy. Every file it
// links to directly beneath it is downloaded, and so are the files in the folders it links to, one level down, for disc
// folders. A URL serving a zip archive is extracted, and any other file is saved as it is.
func Release(ctx context.Context, src string) (dir string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "wrtag-download-*")
	if err != nil {
		return "", nil, fmt.Errorf("make temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	archive, err := fetch(ctx, src, tmp)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if archive == "" {
		return tmp, cleanup, nil
	}

	dir, cleanupZip, err := ziputil.Extract(archive)
	cleanup() // the archive itself is no longer needed
	if err != nil {
		return "", nil, err
	}
	return dir, cleanupZip, nil
}

// fetch fetches the URL src into dir. If it serves a zip archive, the path it was saved to is returned.
func fetch(ctx context.Context, src, dir string) (archive string, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close() //nolint:errcheck

	resp, err := get(ctx, src)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if isListing(resp) {
		return "", downloadListing(ctx, root, ".", resp, 1)
	}

	// recognise a zip by its signature, since download links often have neither a .zip name nor a zip content type
	body := bufio.NewReader(resp.Body)
	if magic, _ := body.Peek(len(zipMagic)); bytes.Equal(magic, zipMagic) {
		const name = "download.zip"
		return filepath.Join(dir, name), saveFile(root, name, body)
	}

	name, err := fileName(resp)
	if err != nil {
		return "", err
	}
	return "", saveFile(root, name, body)
}

var zipMagic = []byte("PK\x03\x04")

// downloadListing downloads the files linked from the directory listing in resp into sub, and recurses into linked
// folders while depth allows.
func downloadListing(ctx context.Context, root *os.Root, sub string, resp *http.Response, depth int) error {
	links, err := listingLinks(resp.Request.URL, resp.Body)
	if err != nil {
		return fmt.Errorf("read listing %s: %w", resp.Request.URL, err)
	}

	for _, link := range links {
		name := path.Base(link.Path)
		isDir := strings.HasSuffix(link.Path, "/")
		if isDir && depth == 0 {
			continue
		}

		if err := func() error {
			resp, err := get(ctx, link.String())
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if !isDir {
				name, err := fileName(resp)
				if err != nil {
					return err
				}
				return saveFile(root, path.Join(sub, name), resp.Body)
			}
			if !isListing(resp) {
				return fmt.Errorf("%s: not a directory listing", link)
			}
			if err := root.Mkdir(path.Join(sub, name), 0o777); err != nil {
				return err
			}
			return downloadListing(ctx, root, path.Join(sub, name), resp, depth-1)
		}(); err != nil {
			return err
		}
	}
	return nil
}

// listingLinks returns the distinct links in the HTML directory listing at base which point to its direct children.
// Other links, like Apache's "Parent Directory" or its "?C=M;O=A" sort links, resolve to base itself or above it.
func listingLinks(base *url.URL, r io.Reader) ([]*url.URL, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	basePath := base.Path
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}

	var links []*url.URL
	seen := map[string]bool{}
	for n := range doc.Descendants() {
		if n.DataAtom != atom.A {
			continue
		}
		for _, attr := range n.Attr {
			if attr.Key != "href" {
				continue
			}
			link, err := base.Parse(attr.Val)
			if err != nil {
				continue
			}
			link.Fragment = ""
			name, ok := strings.CutPrefix(link.Path, basePath)
			name = strings.TrimSuffix(name, "/")
			if !ok || name == "" || strings.Contains(name, "/") || seen[link.String()] {
				continue
			}
			seen[link.String()] = true
			links = append(links, link)
		}
	}
	return links, nil
}

// fileName returns the name the server asks a download to be saved as with its Content-Disposition header, or else
// the last part of its URL.
func fileName(resp *http.Response) (string, error) {
	name := path.Base(resp.Request.URL.Path)
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = path.Base(strings.ReplaceAll(params["filename"], `\`, "/"))
	}
	if name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("%s: no file name", resp.Request.URL)
	}
	return name, nil
}

func saveFile(root *os.Root, name string, r io.Reader) error {
	f, err := root.Create(name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("download %q: %w", name, err)
	}
	return f.Close()
}

func get(ctx context.Context, src string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("get %s: %s", src, resp.Status)
	}
	return resp, nil
}

func isListing(resp *http.Response) bool {
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return mt == "text/html"
}
