package gui

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// An exported iPad copy is deliberately independent of the download library.
// Nothing here removes or modifies the original movie or episode.
type ipadJob struct {
	ID       string             `json:"id"`
	Status   string             `json:"status"`
	Percent  int                `json:"percent"`
	FileName string             `json:"fileName"`
	Size     int64              `json:"size,omitempty"`
	Error    string             `json:"error,omitempty"`
	Output   string             `json:"-"`
	Source   string             `json:"-"`
	Cancel   context.CancelFunc `json:"-"`
}

type ipadProbe struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		PixFmt       string `json:"pix_fmt"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		BitRate      string `json:"bit_rate"`
		Channels     int    `json:"channels"`
		SampleRate   string `json:"sample_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func ipadProbeFile(ctx context.Context, filename string) (ipadProbe, error) {
	var p ipadProbe
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "ffprobe", "-v", "error", "-show_entries",
		"stream=codec_type,codec_name,pix_fmt,width,height,bit_rate,channels,sample_rate,avg_frame_rate:format=duration",
		"-of", "json", filename).Output()
	if err != nil {
		return p, errors.New("ffprobe could not inspect the video")
	}
	if err = json.Unmarshal(out, &p); err != nil {
		return p, errors.New("invalid ffprobe response")
	}
	return p, nil
}

func ipadArgs(src, dst string, p ipadProbe) ([]string, float64, error) {
	var video, audio *struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		PixFmt       string `json:"pix_fmt"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		BitRate      string `json:"bit_rate"`
		Channels     int    `json:"channels"`
		SampleRate   string `json:"sample_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
	}
	for i := range p.Streams {
		x := &p.Streams[i]
		if x.CodecType == "video" && video == nil {
			video = x
		}
		if x.CodecType == "audio" && audio == nil {
			audio = x
		}
	}
	if video == nil {
		return nil, 0, errors.New("no video stream")
	}
	duration, _ := strconv.ParseFloat(p.Format.Duration, 64)
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-y", "-i", src,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-map_metadata", "-1"}
	bitrate, _ := strconv.ParseInt(video.BitRate, 10, 64)
	fps := 0.0
	if parts := strings.Split(video.AvgFrameRate, "/"); len(parts) == 2 {
		numerator, _ := strconv.ParseFloat(parts[0], 64)
		denominator, _ := strconv.ParseFloat(parts[1], 64)
		if denominator > 0 {
			fps = numerator / denominator
		}
	}
	// Video copy when the source is already iPad-friendly and reasonably sized.
	fast := video.CodecName == "h264" && video.PixFmt == "yuv420p" &&
		video.Width > 0 && video.Height > 0 && video.Width <= 1920 && video.Height <= 1080 &&
		(fps == 0 || fps <= 60) && (bitrate == 0 || bitrate <= 6_000_000)
	if fast {
		args = append(args, "-c:v", "copy", "-tag:v", "avc1")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
			"-pix_fmt", "yuv420p", "-vf", "scale=w='min(1920,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2",
			"-threads", "2")
	}
	// Stereo AAC for the native Files/Photos player. Keep first audio track only.
	if audio != nil {
		audioBitrate, _ := strconv.ParseInt(audio.BitRate, 10, 64)
		sampleRate, _ := strconv.Atoi(audio.SampleRate)
		if audio.CodecName == "aac" && audio.Channels > 0 && audio.Channels <= 2 &&
			(sampleRate == 0 || sampleRate <= 48000) &&
			(audioBitrate == 0 || audioBitrate <= 192_000) {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "160k")
		}
	}
	args = append(args, "-movflags", "+faststart", "-f", "mp4",
		"-progress", "pipe:1", "-nostats", dst)
	return args, duration, nil
}

// All prepared copies live on the state volume, separate from the raw downloads.
func ipadExportsRoot() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("HOME not configured")
	}
	return filepath.Join(home, "iPad-Exports"), nil
}

func ipadOutput(real string, info os.FileInfo) (string, string, error) {
	root, err := ipadExportsRoot()
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", real, info.Size(), info.ModTime().UnixNano())))
	id := hex.EncodeToString(hash[:16])
	stem := strings.TrimSuffix(filepath.Base(real), filepath.Ext(real))
	// Avoid surprising control characters in Content-Disposition filenames.
	stem = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, stem)
	if len(stem) > 150 {
		stem = string([]rune(stem)[:min(len([]rune(stem)), 65)])
	}
	if stem == "" {
		stem = "video"
	}
	out := filepath.Join(root, id, stem+"-iPad.mp4")
	return id, out, nil
}

func (s *Server) ipadJobSnapshot(id string) (ipadJob, bool) {
	s.ipadMu.Lock()
	defer s.ipadMu.Unlock()
	j, ok := s.ipadJobs[id]
	if !ok {
		return ipadJob{}, false
	}
	return *j, true
}

func (s *Server) handleIPadPrepare(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	real, info, err := s.localMediaPath(input.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, "video not in library")
		return
	}
	id, out, err := ipadOutput(real, info)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "iPad storage unavailable")
		return
	}

	s.ipadMu.Lock()
	if old, ok := s.ipadJobs[id]; ok && (old.Status == "running" || old.Status == "ready") {
		copy := *old
		s.ipadMu.Unlock()
		if copy.Status == "ready" {
			if stat, e := os.Stat(copy.Output); e == nil && stat.Size() > 0 {
				writeJSON(w, http.StatusOK, copy)
				return
			}
		} else {
			writeJSON(w, http.StatusOK, copy)
			return
		}
		s.ipadMu.Lock()
	}
	if s.ipadActive != "" {
		s.ipadMu.Unlock()
		writeErr(w, http.StatusConflict, "Сейчас готовится другой фильм для iPad")
		return
	}
	// Existing complete MP4s are durable and need no regeneration after restart.
	if st, e := os.Stat(out); e == nil && st.Mode().IsRegular() && st.Size() > 0 {
		j := &ipadJob{ID: id, Status: "ready", Percent: 100, FileName: filepath.Base(out), Size: st.Size(), Output: out, Source: real}
		s.ipadJobs[id] = j
		copy := *j
		s.ipadMu.Unlock()
		writeJSON(w, http.StatusOK, copy)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &ipadJob{ID: id, Status: "running", Percent: 0, FileName: filepath.Base(out), Output: out, Source: real, Cancel: cancel}
	s.ipadJobs[id] = j
	s.ipadActive = id
	copy := *j
	s.ipadMu.Unlock()
	go s.runIPadExport(ctx, j)
	writeJSON(w, http.StatusOK, copy)
}

func (s *Server) runIPadExport(ctx context.Context, j *ipadJob) {
	defer func() {
		s.ipadMu.Lock()
		if s.ipadActive == j.ID {
			s.ipadActive = ""
		}
		s.ipadMu.Unlock()
	}()
	fail := func(msg string) {
		s.ipadMu.Lock()
		if j.Status != "canceled" {
			j.Status = "failed"
			j.Error = msg
		}
		s.ipadMu.Unlock()
	}
	p, err := ipadProbeFile(ctx, j.Source)
	if err != nil {
		fail("Не удалось проверить исходный файл (ffprobe)")
		return
	}
	if err = os.MkdirAll(filepath.Dir(j.Output), 0700); err != nil {
		fail("Нет доступа к папке iPad-Exports")
		return
	}
	tmp := j.Output + ".part"
	defer os.Remove(tmp)
	args, duration, err := ipadArgs(j.Source, tmp, p)
	if err != nil {
		fail("Файл не содержит видеодорожку")
		return
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail("Ошибка запуска FFmpeg")
		return
	}
	// FFmpeg errors are deliberately not logged (paths and metadata may be private).
	if err = cmd.Start(); err != nil {
		fail("FFmpeg недоступен")
		return
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		key, val, ok := strings.Cut(line, "=")
		if !ok || (key != "out_time_us" && key != "out_time_ms") || duration <= 0 {
			continue
		}
		us, parseErr := strconv.ParseFloat(val, 64)
		if parseErr != nil {
			continue
		}
		percent := int(100 * (us / 1_000_000) / duration)
		if percent < 0 {
			percent = 0
		}
		if percent > 99 {
			percent = 99
		}
		s.ipadMu.Lock()
		if j.Status == "running" && percent > j.Percent {
			j.Percent = percent
		}
		s.ipadMu.Unlock()
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		fail("Обработка отменена")
		return
	}
	if err != nil {
		fail("Ошибка FFmpeg. Проверьте формат и свободное место.")
		return
	}
	out, err := os.Stat(tmp)
	if err != nil || out.Size() == 0 {
		fail("Готовый файл не создан")
		return
	}
	if err = os.Rename(tmp, j.Output); err != nil {
		fail("Не удалось сохранить результат")
		return
	}
	s.ipadMu.Lock()
	j.Status = "ready"
	j.Percent = 100
	j.Size = out.Size()
	s.ipadMu.Unlock()
}

func (s *Server) handleIPadStatus(w http.ResponseWriter, r *http.Request) {
	j, ok := s.ipadJobSnapshot(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "export not found")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) handleIPadCancel(w http.ResponseWriter, r *http.Request) {
	s.ipadMu.Lock()
	j, ok := s.ipadJobs[r.PathValue("id")]
	if !ok {
		s.ipadMu.Unlock()
		writeErr(w, http.StatusNotFound, "export not found")
		return
	}
	if j.Status == "running" {
		j.Status = "canceled"
		j.Error = "Обработка отменена"
		j.Cancel()
	}
	copy := *j
	s.ipadMu.Unlock()
	writeJSON(w, http.StatusOK, copy)
}

func (s *Server) handleIPadDelete(w http.ResponseWriter, r *http.Request) {
	s.ipadMu.Lock()
	j, exists := s.ipadJobs[r.PathValue("id")]
	if !exists {
		s.ipadMu.Unlock()
		writeErr(w, http.StatusNotFound, "export not found")
		return
	}
	if j.Status != "ready" {
		s.ipadMu.Unlock()
		writeErr(w, http.StatusConflict, "file not ready")
		return
	}
	output := j.Output
	s.ipadMu.Unlock()
	root, err := ipadExportsRoot()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage unavailable")
		return
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage unavailable")
		return
	}
	realOutput, err := filepath.EvalSymlinks(output)
	if err != nil || !pathInRoot(realOutput, realRoot) {
		writeErr(w, http.StatusForbidden, "invalid file")
		return
	}
	if err = os.Remove(realOutput); err != nil && !os.IsNotExist(err) {
		writeErr(w, http.StatusInternalServerError, "cannot remove export")
		return
	}
	_ = os.Remove(filepath.Dir(realOutput)) // only if empty, never remove library files
	s.ipadMu.Lock()
	j.Status = "deleted"
	j.Size = 0
	copy := *j
	s.ipadMu.Unlock()
	writeJSON(w, http.StatusOK, copy)
}

func (s *Server) handleIPadDownload(w http.ResponseWriter, r *http.Request) {
	j, ok := s.ipadJobSnapshot(r.PathValue("id"))
	if !ok || j.Status != "ready" {
		writeErr(w, http.StatusNotFound, "file not ready")
		return
	}
	root, err := ipadExportsRoot()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage unavailable")
		return
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		writeErr(w, http.StatusNotFound, "file not found")
		return
	}
	realFile, err := filepath.EvalSymlinks(j.Output)
	if err != nil || !pathInRoot(realFile, realRoot) {
		writeErr(w, http.StatusForbidden, "invalid file")
		return
	}
	file, err := os.Open(realFile)
	if err != nil {
		writeErr(w, http.StatusNotFound, "file not found")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, "invalid file")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", "iPad-video.mp4", url.QueryEscape(j.FileName)))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, j.FileName, stat.ModTime(), file)
}

// All endpoints inherit the NAS authentication and Origin restrictions.
