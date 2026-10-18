package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIPadFileNames(t *testing.T) {
	for _, tc := range []struct{ path, folder, file string }{
		{"/media/Grand Theft Auto V/Season 01/S01E01.mp4", "Grand Theft Auto V", "Grand Theft Auto V - S01E01 - iPad.mp4"},
		{"/media/Любовь твоей жизни/Сезон 01/S01E02.mkv", "Любовь твоей жизни", "Любовь твоей жизни - S01E02 - iPad.mp4"},
		{"/media/Фильм 2026/My Great Movie.mkv", "My Great Movie", "My Great Movie - iPad.mp4"},
	} {
		title, name := ipadFriendlyName(tc.path)
		if title != tc.folder || name != tc.file {
			t.Fatalf("for %q got %q and %q", tc.path, title, name)
		}
	}
}

func TestIPadQueueFIFOAndRestore(t *testing.T) {
	conf := t.TempDir()
	output := t.TempDir()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", conf)
	t.Setenv("KINOPUB_IPAD_OUTPUT_DIR", output)
	s := newTestServer(t)
	s.settings.mu.Lock()
	s.settings.cur.OutputPath = root
	s.settings.mu.Unlock()
	// Block the worker without starting any process to test order deterministically.
	s.ipadMu.Lock()
	s.ipadActive = "paused-by-test"
	s.ipadMu.Unlock()
	var ids []string
	for _, n := range []string{"A", "B", "C"} {
		file := filepath.Join(root, n+".mp4")
		if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"path": file})
		w := httptest.NewRecorder()
		s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s HTTP %d: %s", n, w.Code, w.Body.String())
		}
		var j ipadJob
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		if j.Status != "queued" || j.Position != len(ids)+1 {
			t.Fatalf("queue position for %s: %#v", n, j)
		}
		ids = append(ids, j.ID)
	}
	if len(s.ipadQueue) != 3 {
		t.Fatal("not all jobs were queued")
	}
	// A double-click must not enqueue an identical job twice.
	file := filepath.Join(root, "A.mp4")
	body, _ := json.Marshal(map[string]string{"path": file})
	w := httptest.NewRecorder()
	s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(body)))
	if len(s.ipadQueue) != 3 {
		t.Fatal("duplicate queue entry")
	}
	cancel := httptest.NewRequest("POST", "/api/ipad/cancel/"+ids[1], nil)
	cancel.SetPathValue("id", ids[1])
	w = httptest.NewRecorder()
	s.handleIPadCancel(w, cancel)
	if w.Code != http.StatusOK || len(s.ipadQueue) != 2 || s.ipadQueue[0] != ids[0] || s.ipadQueue[1] != ids[2] {
		t.Fatalf("unexpected queue after cancel: %#v", s.ipadQueue)
	}
	// A second Server instance restores persisted order but doesn't spawn ffmpeg.
	restored := &Server{settings: s.settings, mgr: s.mgr, ipadJobs: make(map[string]*ipadJob), ipadActive: "paused-by-test"}
	restored.restoreIPadQueue()
	if len(restored.ipadQueue) != 2 || restored.ipadQueue[0] != ids[0] || restored.ipadQueue[1] != ids[2] {
		t.Fatalf("restore lost queue order: %#v", restored.ipadQueue)
	}
	if restored.ipadJobs[ids[1]].Status != "canceled" {
		t.Fatal("cancelled job resurrected")
	}
	if title, _ := ipadFriendlyName("/media/Grand Theft Auto V/Season 01/S01E01.mp4"); !strings.Contains(title, "Grand Theft Auto V") {
		t.Fatal("unreadable display title")
	}
}

func TestProfiledOutputDoesNotOverwriteOldFastExport(t *testing.T) {
	t.Setenv("KINOPUB_IPAD_OUTPUT_DIR", t.TempDir())
	f := filepath.Join(t.TempDir(), "Movie.mp4")
	if err := os.WriteFile(f, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(f)
	oldID, oldPath, _ := ipadOutput(f, stat)
	fastID, fastPath, _ := ipadOutputForProfile(f, stat, "fast")
	compactID, compactPath, _ := ipadOutputForProfile(f, stat, "compact")
	if oldID != fastID || oldPath != fastPath {
		t.Fatal("old exports must preserve their identity")
	}
	if compactID == oldID || compactPath == oldPath {
		t.Fatal("compact must never reuse old large file")
	}
	if !strings.Contains(compactPath, "Compact") {
		t.Fatal("compact output must be distinct")
	}
}
