package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIPadTwoWorkersWithStubFFmpegAndRestart(t *testing.T) {
	t.Setenv("KINOPUB_IPAD_CONCURRENT", "2")
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KINOPUB_IPAD_OUTPUT_DIR", t.TempDir())
	probe := `#!/bin/sh
printf '{"streams":[{"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","width":1920,"height":1080,"avg_frame_rate":"24/1"}],"format":{"duration":"5"}}'
`
	codec := `#!/bin/sh
for output; do :; done
sleep 1
printf 'fake MP4' > "$output"
`
	for name, source := range map[string]string{"ffprobe": probe, "ffmpeg": codec} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(source), 0700); err != nil {
			t.Fatal(err)
		}
	}
	s := newTestServer(t)
	root := t.TempDir()
	s.settings.mu.Lock()
	s.settings.cur.OutputPath = root
	s.settings.mu.Unlock()
	var ids []string
	for i := 1; i <= 3; i++ {
		file := filepath.Join(root, fmt.Sprintf("file%d.mkv", i))
		if err := os.WriteFile(file, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"path": file, "profile": "balanced"})
		w := httptest.NewRecorder()
		s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("enqueue %d: %s", i, w.Body.String())
		}
		var job ipadJob
		if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
	}
	s.ipadMu.Lock()
	current, pending := len(s.ipadActive), len(s.ipadQueue)
	s.ipadMu.Unlock()
	if current != 2 || pending != 1 {
		t.Fatalf("expected two running, one queued, got %d/%d", current, pending)
	}
	start := time.Now()
	for {
		s.ipadMu.Lock()
		ready := 0
		for _, id := range ids {
			if s.ipadJobs[id].Status == "ready" {
				ready++
			}
		}
		s.ipadMu.Unlock()
		if ready == 3 {
			break
		}
		if time.Since(start) > 8*time.Second {
			t.Fatalf("did not finish: %d ready", ready)
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Restore from the same persisted queue file, as after restarting the NAS.
	restored := &Server{settings: s.settings, mgr: s.mgr, ipadJobs: make(map[string]*ipadJob), ipadActive: make(map[string]struct{})}
	restored.restoreIPadQueue()
	for _, id := range ids {
		j, ok := restored.ipadJobSnapshot(id)
		if !ok || j.Status != "ready" {
			t.Fatalf("missing ready job on restore %q %v", id, ok)
		}
	}
	w := httptest.NewRecorder()
	restored.handleIPadQueue(w, httptest.NewRequest("GET", "/api/ipad/queue", nil))
	if strings.Count(w.Body.String(), "ready") != 3 {
		t.Fatalf("restore result: %s", w.Body.String())
	}
}
