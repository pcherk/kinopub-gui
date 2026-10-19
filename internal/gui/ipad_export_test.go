package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIPadArgsProfiles(t *testing.T) {
	p := ipadProbe{}
	p.Streams = append(p.Streams, struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		PixFmt       string `json:"pix_fmt"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		BitRate      string `json:"bit_rate"`
		Channels     int    `json:"channels"`
		SampleRate   string `json:"sample_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
	}{CodecType: "video", CodecName: "h264", PixFmt: "yuv420p", Width: 1920, Height: 1080, BitRate: "4500000", AvgFrameRate: "30/1"})
	p.Format.Duration = "123.45"
	args, dur, err := ipadArgs("in.mkv", "out.mp4.part", p, "fast")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if dur != 123.45 || !strings.Contains(joined, "-c:v copy") {
		t.Fatalf("expected stream copy: %s", joined)
	}
	compact, _, err := ipadArgs("in.mkv", "out.mp4.part", p, "compact")
	if err != nil || !strings.Contains(strings.Join(compact, " "), "-maxrate 1800k") ||
		!strings.Contains(strings.Join(compact, " "), "-c:v libx264") ||
		!strings.Contains(strings.Join(compact, " "), "min(720,ih)") {
		t.Fatal("compact must reencode at 720p with bitrate cap")
	}
	balanced, _, err := ipadArgs("in.mkv", "out.mp4.part", p, "balanced")
	if err != nil || !strings.Contains(strings.Join(balanced, " "), "-crf 21") ||
		!strings.Contains(strings.Join(balanced, " "), "-maxrate 4000k") ||
		!strings.Contains(strings.Join(balanced, " "), "min(1080,ih)") ||
		!strings.Contains(strings.Join(balanced, " "), "-preset veryfast") {
		t.Fatalf("balanced profile must preserve 1080p and use gentler compression: %v", balanced)
	}
	p.Streams[0].CodecName = "hevc"
	args, _, err = ipadArgs("in.mkv", "out.mp4.part", p, "fast")
	if err != nil || !strings.Contains(strings.Join(args, " "), "-c:v libx264") {
		t.Fatal("HEVC must be transcoded")
	}
	p.Streams[0].CodecType = "subtitle"
	_, _, err = ipadArgs("in.mkv", "out.mp4.part", p, "fast")
	if err == nil {
		t.Fatal("no video must fail")
	}
}

func TestIPadExportRejectsOutsideFile(t *testing.T) {
	s := newTestServer(t)
	root := t.TempDir()
	s.settings.mu.Lock()
	s.settings.cur.OutputPath = root
	s.settings.mu.Unlock()
	other := filepath.Join(t.TempDir(), "secret.mp4")
	if err := os.WriteFile(other, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": other})
	w := httptest.NewRecorder()
	s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(body)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("outside HTTP %d", w.Code)
	}
}

func TestIPadConvertAndDownloadRealFFmpeg(t *testing.T) {
	if os.Getenv("KINOPUB_FFMPEG_INTEGRATION") != "1" {
		t.Skip("run within FFmpeg runtime image")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	s := newTestServer(t)
	root := t.TempDir()
	s.settings.mu.Lock()
	s.settings.cur.OutputPath = root
	s.settings.mu.Unlock()

	src := filepath.Join(root, "фильм.mkv")
	ffmpeg := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "2", "-c:v", "mpeg4", "-q:v", "4", "-c:a", "aac", src)
	if out, err := ffmpeg.CombinedOutput(); err != nil {
		t.Fatalf("test fixture failed: %v %s", err, out)
	}
	orig, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	reqBytes, _ := json.Marshal(map[string]string{"path": src})
	sendPrepare := func() ipadJob {
		w := httptest.NewRecorder()
		s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(reqBytes)))
		if w.Code != http.StatusOK {
			t.Fatalf("start status %d: %s", w.Code, w.Body.String())
		}
		var job ipadJob
		if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	got := sendPrepare()
	timeout := time.After(30 * time.Second)
	for got.Status == "running" {
		select {
		case <-timeout:
			t.Fatalf("conversion timed out at %d%%", got.Percent)
		default:
		}
		time.Sleep(100 * time.Millisecond)
		v, ok := s.ipadJobSnapshot(got.ID)
		if !ok {
			t.Fatal("missing conversion")
		}
		got = v
	}
	if got.Status != "ready" || got.Percent != 100 {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	if strings.ToLower(filepath.Ext(got.Output)) != ".mp4" {
		t.Fatal("not an MP4 output")
	}
	p, err := ipadProbeFile(context.Background(), got.Output)
	if err != nil {
		t.Fatal(err)
	}
	if p.Streams[0].CodecName != "h264" {
		t.Fatal("wrong output video codec")
	}
	got2 := sendPrepare()
	if got2.ID != got.ID || got2.Status != "ready" || got2.Size != got.Size {
		t.Fatal("completed conversion was not reused")
	}
	if check, err := os.Stat(src); err != nil || orig.Size() != check.Size() {
		t.Fatal("source file was changed")
	}

	url := "/api/ipad/download/" + got.ID
	r := httptest.NewRequest("GET", url, nil)
	r.SetPathValue("id", got.ID)
	r.Header.Set("Range", "bytes=0-15")
	w := httptest.NewRecorder()
	s.handleIPadDownload(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("download HTTP %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("download headers missing")
	}
	if w.Body.Len() != 16 {
		t.Fatalf("range size=%d", w.Body.Len())
	}

	// Symlinks cannot turn a downloaded movie request into arbitrary file access.
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.mp4")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	bad, _ := json.Marshal(map[string]string{"path": link})
	w = httptest.NewRecorder()
	s.handleIPadPrepare(w, httptest.NewRequest("POST", "/api/ipad/prepare", bytes.NewReader(bad)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("symlink HTTP %d", w.Code)
	}
	deletedReq := httptest.NewRequest("POST", "/api/ipad/delete/"+got.ID, nil)
	deletedReq.SetPathValue("id", got.ID)
	deleted := httptest.NewRecorder()
	s.handleIPadDelete(deleted, deletedReq)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(got.Output); !os.IsNotExist(err) {
		t.Fatalf("export was not deleted: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source was deleted: %v", err)
	}
}
