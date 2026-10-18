package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var localSegments = regexp.MustCompile(`^seg[0-9]{5}\.ts$`)

type localHLSStream struct {
	dir     string
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	touched time.Time
}

var localHLSState = struct {
	sync.Mutex
	streams map[string]*localHLSStream
}{streams: make(map[string]*localHLSStream)}

var localHLSJanitor sync.Once

// Retire completed conversion caches and cancel abandoned conversions in the
// background. Stale segment directories from previous container processes are
// removed when local playback is first requested after a restart.
func initLocalHLSCache() {
	localHLSJanitor.Do(func() {
		if cacheRoot, err := os.UserCacheDir(); err == nil {
			root := filepath.Join(cacheRoot, "kinopub-local-playback")
			if entries, err := os.ReadDir(root); err == nil {
				for _, ent := range entries {
					if ent.IsDir() && strings.HasPrefix(ent.Name(), "stream-") {
						_ = os.RemoveAll(filepath.Join(root, ent.Name()))
					}
				}
			}
		}
		go func() {
			tick := time.NewTicker(5 * time.Minute)
			defer tick.Stop()
			for range tick.C {
				retireLocalHLSCache()
			}
		}()
	})
}

func retireLocalHLSCache() {
	var retired []*localHLSStream
	localHLSState.Lock()
	for key, st := range localHLSState.streams {
		if time.Since(st.touched) < 30*time.Minute {
			continue
		}
		st.cancel()
		delete(localHLSState.streams, key)
		retired = append(retired, st)
	}
	localHLSState.Unlock()
	for _, st := range retired {
		go func(x *localHLSStream) { <-x.done; _ = os.RemoveAll(x.dir) }(st)
	}
}

func pathInRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// Resolve target and each configured root, including symlinks, before opening
// any file. The older /api/open lexical check is NOT sufficient for streaming.
func (s *Server) localMediaPath(p string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(p) {
		return "", nil, errors.New("absolute path required")
	}
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".mp4", ".m4v", ".webm", ".mkv":
	default:
		return "", nil, errors.New("unsupported video format")
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", nil, err
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("not a regular video file")
	}
	roots := append(s.libraryDirs(), s.mgr.outputPaths()...)
	for _, root := range roots {
		if root == "" {
			continue
		}
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rr, err = filepath.Abs(rr)
		if err == nil && pathInRoot(real, rr) {
			return real, info, nil
		}
	}
	return "", nil, errors.New("video is outside downloaded library")
}

func (s *Server) handleLocalVideo(w http.ResponseWriter, r *http.Request) {
	real, info, err := s.localMediaPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "video is not available", http.StatusForbidden)
		return
	}
	ext := strings.ToLower(filepath.Ext(real))
	if ext == ".mkv" {
		http.Error(w, "MKV requires /api/local/hls", http.StatusUnsupportedMediaType)
		return
	}
	f, err := os.Open(real)
	if err != nil {
		http.Error(w, "video is not available", http.StatusNotFound)
		return
	}
	defer f.Close()
	ct := mime.TypeByExtension(ext)
	if ext == ".mp4" || ext == ".m4v" {
		ct = "video/mp4"
	}
	if ext == ".webm" {
		ct = "video/webm"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(real), info.ModTime(), f) // supports HTTP Range
}

func localHLSKey(real string, stat os.FileInfo) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", real, stat.Size(), stat.ModTime().UnixNano())))
	return hex.EncodeToString(h[:])
}

type ffprobeVideo struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
	} `json:"streams"`
}

func localVideoCodec(real string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name", "-of", "json", real).Output()
	if err != nil {
		return ""
	}
	var probe ffprobeVideo
	if json.Unmarshal(b, &probe) != nil || len(probe.Streams) == 0 {
		return ""
	}
	return probe.Streams[0].CodecName
}

func startLocalHLS(real, dir string) *localHLSStream {
	ctx, cancel := context.WithCancel(context.Background())
	st := &localHLSStream{dir: dir, cancel: cancel, done: make(chan struct{}), touched: time.Now()}
	codec := localVideoCodec(real)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y", "-i", real, "-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn"}
	if codec == "h264" {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "24", "-threads", "2")
	}
	args = append(args, "-c:a", "aac", "-b:a", "160k",
		"-f", "hls", "-hls_time", "6", "-hls_list_size", "0", "-hls_playlist_type", "event",
		"-hls_flags", "independent_segments+temp_file",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		filepath.Join(dir, "index.m3u8"))
	go func() {
		defer close(st.done)
		// Do not log stderr: media paths may contain sensitive filenames.
		st.err = exec.CommandContext(ctx, "ffmpeg", args...).Run()
	}()
	return st
}

func prepareLocalHLS(real string, stat os.FileInfo) (*localHLSStream, error) {
	initLocalHLSCache()
	key := localHLSKey(real, stat)
	localHLSState.Lock()
	defer localHLSState.Unlock()
	now := time.Now()
	// Only one active conversion at a time; do not overwhelm Synology CPU.
	for id, stream := range localHLSState.streams {
		select {
		case <-stream.done:
			if now.Sub(stream.touched) > 30*time.Minute {
				delete(localHLSState.streams, id)
				os.RemoveAll(stream.dir)
			}
		default:
			if now.Sub(stream.touched) > 15*time.Minute {
				stream.cancel()
				delete(localHLSState.streams, id)
				os.RemoveAll(stream.dir)
			} else if id != key {
				return nil, errors.New("another video is being prepared; try again later")
			}
		}
	}
	if st := localHLSState.streams[key]; st != nil {
		st.touched = now
		return st, nil
	}
	// Store only generated streaming data in the existing state volume.
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(cacheRoot, "kinopub-local-playback")
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "stream-")
	if err != nil {
		return nil, err
	}
	st := startLocalHLS(real, dir)
	localHLSState.streams[key] = st
	return st, nil
}

func (s *Server) handleLocalHLS(w http.ResponseWriter, r *http.Request) {
	real, stat, err := s.localMediaPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "video is not available", http.StatusForbidden)
		return
	}
	part := r.URL.Query().Get("part")
	if part == "" {
		part = "index.m3u8"
	}
	if part != "index.m3u8" && !localSegments.MatchString(part) {
		http.Error(w, "invalid segment", http.StatusBadRequest)
		return
	}
	st, err := prepareLocalHLS(real, stat)
	if err != nil {
		http.Error(w, "video preparation unavailable", http.StatusServiceUnavailable)
		return
	}
	target := filepath.Join(st.dir, part)
	// The requested segment or playlist may not yet exist while FFmpeg encodes.
	deadline := time.NewTimer(18 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		f, openErr := os.Open(target)
		if openErr == nil {
			info, statErr := f.Stat()
			if statErr == nil && info.Mode().IsRegular() {
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				if part == "index.m3u8" {
					data, readErr := io.ReadAll(io.LimitReader(f, 2<<20))
					f.Close()
					if readErr != nil {
						http.Error(w, "playlist unavailable", 500)
						return
					}
					var b strings.Builder
					for _, line := range strings.Split(string(data), "\n") {
						if localSegments.MatchString(strings.TrimSpace(line)) {
							b.WriteString("/api/local/hls?path=")
							b.WriteString(url.QueryEscape(r.URL.Query().Get("path")))
							b.WriteString("&part=")
							b.WriteString(strings.TrimSpace(line))
						} else {
							b.WriteString(line)
						}
						b.WriteByte('\n')
					}
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					io.WriteString(w, b.String())
					return
				}
				w.Header().Set("Content-Type", "video/mp2t")
				http.ServeContent(w, r, part, info.ModTime(), f)
				f.Close()
				return
			}
			f.Close()
		}
		select {
		case <-r.Context().Done():
			return
		case <-st.done:
			// The encoder has exited. No more segments will arrive.
			if _, err = os.Stat(target); err != nil {
				http.Error(w, "segment unavailable", http.StatusNotFound)
				return
			}
		case <-deadline.C:
			http.Error(w, "video is still preparing", http.StatusServiceUnavailable)
			return
		case <-tick.C:
		}
	}
}
