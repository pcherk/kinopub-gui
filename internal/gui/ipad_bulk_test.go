package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func TestIPadWorkerCount(t *testing.T) {
	t.Setenv("KINOPUB_IPAD_CONCURRENT", "1")
	if ipadConcurrency() != 1 {
		t.Fatal("expected one")
	}
	t.Setenv("KINOPUB_IPAD_CONCURRENT", "2")
	if ipadConcurrency() != 2 {
		t.Fatal("expected two")
	}
	t.Setenv("KINOPUB_IPAD_CONCURRENT", "999")
	if ipadConcurrency() != 2 {
		t.Fatal("unsafe concurrency")
	}
}

func TestIPadAllAndStaleRecords(t *testing.T) {
	t.Setenv("KINOPUB_IPAD_OUTPUT_DIR", t.TempDir())
	s := newTestServer(t)
	root := t.TempDir()
	s.settings.mu.Lock()
	s.settings.cur.OutputPath = root
	s.settings.mu.Unlock()
	s.ipadMu.Lock()
	s.ipadActive = map[string]struct{}{"pause1": {}, "pause2": {}}
	s.ipadMu.Unlock()
	seriesDir := filepath.Join(root, "Movie")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]domain.CompletedRec{}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("S01E%02d.mp4", i)
		if err := os.WriteFile(filepath.Join(seriesDir, name), []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		files[fmt.Sprintf("S1E%d", i)] = domain.CompletedRec{Season: 1, Episode: i, Path: name}
	}
	writeStateFile(t, seriesDir, domain.DownloadState{
		Metadata:  &domain.SeriesMetadata{Title: "Movie", OriginalTitle: "Movie", Type: "serial"},
		Completed: files,
	})
	enqueue := func() map[string]any {
		w := httptest.NewRecorder()
		s.handleIPadPrepareAll(w, httptest.NewRequest("POST", "/api/ipad/prepare-all", bytes.NewBufferString(`{"profile":"balanced"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("bulk HTTP %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if v := enqueue(); v["added"] != float64(3) {
		t.Fatalf("not all added: %v", v)
	}
	if v := enqueue(); v["added"] != float64(0) {
		t.Fatalf("duplicate enqueue: %v", v)
	}
	s.ipadMu.Lock()
	if len(s.ipadQueue) != 3 {
		t.Fatal("queue has wrong length")
	}
	var ids []string
	for id := range s.ipadJobs {
		ids = append(ids, id)
	}
	valid := s.ipadJobs[ids[0]]
	invalid := s.ipadJobs[ids[1]]
	valid.Status = "ready"
	invalid.Status = "ready"
	for _, id := range ids[:2] {
		for i, key := range s.ipadQueue {
			if key == id {
				s.ipadQueue = append(s.ipadQueue[:i], s.ipadQueue[i+1:]...)
				break
			}
		}
	}
	s.ipadMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(valid.Output), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(valid.Output, []byte("fake exported mp4"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleIPadQueue(w, httptest.NewRequest("GET", "/api/ipad/queue", nil))
	if len(s.ipadJobs) != 2 {
		t.Fatalf("stale export not pruned: %d", len(s.ipadJobs))
	}
	w = httptest.NewRecorder()
	s.handleIPadClear(w, httptest.NewRequest("POST", "/api/ipad/clear", nil))
	if len(s.ipadJobs) != 2 {
		t.Fatal("ready and queued records must remain")
	}
	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(filepath.Join(seriesDir, fmt.Sprintf("S01E%02d.mp4", i))); err != nil {
			t.Fatalf("original file altered: %v", err)
		}
	}
}
