package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This optional test uses real FFmpeg to verify MKV -> HLS on the target Linux
// image. Default unit tests remain fast when FFmpeg isn't installed.
func TestLocalMKVRealFFmpeg(t *testing.T) {
	if os.Getenv("KINOPUB_FFMPEG_INTEGRATION") != "1" {
		t.Skip("run with FFmpeg image")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := newTestServer(t)
	root := t.TempDir()
	srv.settings.mu.Lock()
	srv.settings.cur.OutputPath = root
	srv.settings.mu.Unlock()
	file := filepath.Join(root, "sample.mkv")
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x90:rate=24", "-f", "lavfi", "-i", "sine=frequency=800:sample_rate=44100", "-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-y", file).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	qr := "/api/local/hls?path=" + urlEscapeForTest(file)
	request := httptest.NewRequest("GET", qr, nil)
	w := httptest.NewRecorder()
	srv.handleLocalHLS(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("playlist code=%d body=%s", w.Code, w.Body.String())
	}
	playlist := w.Body.String()
	if !strings.Contains(playlist, "#EXTM3U") || !strings.Contains(playlist, "/api/local/hls?") {
		t.Fatalf("playlist malformed: %s", playlist)
	}
	var url string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "/api/local/hls?") {
			url = line
			break
		}
	}
	if url == "" {
		t.Fatal("missing media segments")
	}
	segmentReq := httptest.NewRequest("GET", url, nil)
	seg := httptest.NewRecorder()
	srv.handleLocalHLS(seg, segmentReq)
	if seg.Code != http.StatusOK {
		t.Fatalf("segment code=%d body=%s", seg.Code, seg.Body.String())
	}
	if seg.Body.Len() == 0 || seg.Body.Bytes()[0] != 0x47 {
		t.Fatalf("invalid transport stream segment: %d bytes", seg.Body.Len())
	}
	// Ensure background encoder has time to finish before temp directory cleanup.
	key := localHLSKey(file, mustStat(t, file))
	localHLSState.Lock()
	st := localHLSState.streams[key]
	localHLSState.Unlock()
	if st != nil {
		select {
		case <-st.done:
		case <-time.After(20 * time.Second):
			st.cancel()
			t.Fatal("ffmpeg did not finish")
		}
		st.cancel()
	}
}
func mustStat(t *testing.T, p string) os.FileInfo {
	t.Helper()
	v, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
