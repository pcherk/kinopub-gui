package gui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalVideoRangeAndPathSecurity(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()
	srv.settings.mu.Lock()
	srv.settings.cur.OutputPath = root
	srv.settings.mu.Unlock()

	file := filepath.Join(root, "Фильм с пробелами.mp4")
	if err := os.WriteFile(file, []byte("0123456789ABCDE"), 0600); err != nil {
		t.Fatal(err)
	}
	request := func(path, rangeHdr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/local/video?path="+urlEscapeForTest(path), nil)
		if rangeHdr != "" {
			req.Header.Set("Range", rangeHdr)
		}
		rr := httptest.NewRecorder()
		srv.handleLocalVideo(rr, req)
		return rr
	}
	rr := request(file, "bytes=2-6")
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("range HTTP %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "23456" {
		t.Fatalf("range bytes = %q", got)
	}
	if rr.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("wrong video content type")
	}
	if rr.Header().Get("Content-Range") != "bytes 2-6/15" {
		t.Fatalf("Content-Range: %s", rr.Header().Get("Content-Range"))
	}
	rr = request(file, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("full GET: %d", rr.Code)
	}
	data, _ := io.ReadAll(rr.Result().Body)
	if string(data) != "0123456789ABCDE" {
		t.Fatal("full content changed")
	}

	outside := filepath.Join(t.TempDir(), "private.mp4")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := request(outside, "").Code; got != http.StatusForbidden {
		t.Fatalf("outside HTTP %d", got)
	}
	link := filepath.Join(root, "link.mp4")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if got := request(link, "").Code; got != http.StatusForbidden {
		t.Fatalf("symlink escape HTTP %d", got)
	}
	if got := request(filepath.Join(root, "missing.mp4"), "").Code; got != http.StatusForbidden {
		t.Fatalf("missing file HTTP %d", got)
	}
	if got := request(filepath.Join(root, "settings.json"), "").Code; got != http.StatusForbidden {
		t.Fatalf("wrong extension HTTP %d", got)
	}
	if got := request(filepath.Join(root, "/../../etc/passwd"), "").Code; got != http.StatusForbidden {
		t.Fatalf("traversal HTTP %d", got)
	}
}

func TestLocalHLSRejectsInvalidSegmentsAndOutsidePaths(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()
	srv.settings.mu.Lock()
	srv.settings.cur.OutputPath = root
	srv.settings.mu.Unlock()
	file := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(file, []byte("not actually a video"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../config.json", "/etc/passwd", "segment.ts", "seg1.ts", "seg00001.ts/../../secrets"} {
		req := httptest.NewRequest("GET", "/api/local/hls?path="+urlEscapeForTest(file)+"&part="+urlEscapeForTest(bad), nil)
		w := httptest.NewRecorder()
		srv.handleLocalHLS(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("part %q: HTTP %d", bad, w.Code)
		}
	}
	outside := filepath.Join(t.TempDir(), "other.mkv")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/local/hls?path="+urlEscapeForTest(outside), nil)
	w := httptest.NewRecorder()
	srv.handleLocalHLS(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("outside MKV HTTP %d", w.Code)
	}
}

func urlEscapeForTest(path string) string {
	return strings.ReplaceAll(url.QueryEscape(path), "+", "%20")
}
