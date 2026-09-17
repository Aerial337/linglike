//go:build windows

package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aerial337/linglike/internal/render"
)

// htmlHost writes rendered pages to temporary files so that the embedded
// browser control can display them via file:// URLs. A fresh file name is
// used for every page to defeat the browser cache.
type htmlHost struct {
	mu   sync.Mutex
	dir  string
	n    int
	last map[string]string // prefix -> last file written
}

func newHTMLHost() (*htmlHost, error) {
	dir := filepath.Join(os.TempDir(), "Linglike")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	// remove leftovers from earlier runs
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".html") {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return &htmlHost{dir: dir, last: map[string]string{}}, nil
}

// write stores the page and returns its file URL.
func (h *htmlHost) write(prefix string, p *render.Page) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	name := fmt.Sprintf("%s-%d.html", prefix, h.n)
	path := filepath.Join(h.dir, name)
	// Mark of the Web puts the page in the Internet zone so that the
	// small script for collapsing sections is allowed to run.
	content := "<!-- saved from url=(0014)about:internet -->\n" + render.HTML(p)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	if old := h.last[prefix]; old != "" && old != path {
		os.Remove(old)
	}
	h.last[prefix] = path
	return fileURL(path), nil
}

func (h *htmlHost) cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.last {
		os.Remove(p)
	}
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// entryFromURL extracts the headword from an entry:// link clicked in a
// page. The browser may or may not percent-encode it.
func entryFromURL(u string) (string, bool) {
	lower := strings.ToLower(u)
	var rest string
	switch {
	case strings.HasPrefix(lower, "entry://"):
		rest = u[len("entry://"):]
	case strings.HasPrefix(lower, "entry:"):
		rest = u[len("entry:"):]
	default:
		return "", false
	}
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimSuffix(rest, "/")
	if dec, err := url.PathUnescape(rest); err == nil {
		rest = dec
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}
