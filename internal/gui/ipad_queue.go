package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var ipadEpisodePattern = regexp.MustCompile(`(?i)S[0-9]{1,2}E[0-9]{1,3}`)
var ipadSeasonPattern = regexp.MustCompile(`(?i)^(season|сезон)[ ._-]*[0-9]{1,2}$`)

func ipadFriendlyName(real string) (string, string) {
	stem := strings.TrimSuffix(filepath.Base(real), filepath.Ext(real))
	dir := filepath.Dir(real)
	title := stem
	if ipadSeasonPattern.MatchString(filepath.Base(dir)) {
		title = filepath.Base(filepath.Dir(dir))
	}
	if ipadEpisodePattern.MatchString(stem) && title == stem {
		title = filepath.Base(dir)
	}
	clean := func(in string) string {
		in = strings.Map(func(r rune) rune {
			if r < 32 || r == 127 || (strings.ContainsRune("<>:|?*", r) || r == 34 || r == 47 || r == 92) {
				return '-'
			}
			return r
		}, in)
		in = strings.TrimSpace(strings.Trim(in, ". "))
		chars := []rune(in)
		if len(chars) > 90 {
			in = string(chars[:90])
		}
		if in == "" || in == ".." {
			return "Фильм"
		}
		return in
	}
	title = clean(title)
	episode := strings.ToUpper(ipadEpisodePattern.FindString(stem))
	file := title
	if episode != "" {
		file += " - " + episode
	}
	return title, file + " - iPad.mp4"
}

// Export names are derived from the library's verified metadata rather than
// guessing that every S01E01 file is a television episode. KinoPub also uses
// S01E01.mp4 for individual movies.
func ipadPrettyName(real string) (folder, filename string) {
	title, fallback := ipadFriendlyName(real)
	movie := false
	metadata := false
	code := ipadEpisodePattern.FindString(filepath.Base(real))
	if dir := filepath.Dir(real); dir != "" {
		for level := 0; level < 4; level++ {
			stateFile := filepath.Join(dir, stateFileName)
			if item, ok := readLibraryState(stateFile); ok {
				matched := false
				var season, episode int
				for _, e := range item.Episodes {
					if filepath.Clean(e.Path) == filepath.Clean(real) {
						matched = true
						season, episode = e.Season, e.Episode
						break
					}
				}
				if matched {
					metadata = true
					movie = item.IsMovie
					title = ipadMetadataTitle(item.Title, item.OriginalTitle)
					if !movie {
						if season > 0 && episode > 0 {
							code = fmt.Sprintf("S%02dE%02d", season, episode)
						}
					}
					break
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if !metadata { // Standalone imports retain the legacy episode heuristic.
		title = ipadNormalizeTitle(title)
	}
	if title == "" {
		title = "Фильм"
	}
	if !movie && code != "" {
		title = strings.TrimSpace(title)
		fallback = title + " - " + strings.ToUpper(code)
	}
	if movie || code == "" {
		fallback = title
	}
	return title, fallback + " - iPad 1080p.mp4"
}

func ipadNormalizeTitle(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	// Legacy KinoPub download directories use "Родное _ Original" names.
	// Avoid blindly replacing underscores embedded inside actual titles.
	for _, sep := range []string{" _ ", " / "} {
		if parts := strings.SplitN(s, sep, 2); len(parts) == 2 &&
			strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
			s = strings.TrimSpace(parts[0]) + " (" + strings.TrimSpace(parts[1]) + ")"
			break
		}
	}
	return ipadSafeName(s)
}

func ipadMetadataTitle(title, original string) string {
	title = strings.Join(strings.Fields(title), " ")
	original = strings.Join(strings.Fields(original), " ")
	for _, sep := range []string{" _ ", " / "} {
		if original != "" && len(title) >= len(sep+original) &&
			strings.HasSuffix(strings.ToLower(title), strings.ToLower(sep+original)) {
			title = strings.TrimSpace(title[:len(title)-len(sep+original)])
			break
		}
	}
	if title == "" {
		title = original
	}
	if original != "" && !strings.EqualFold(title, original) && !strings.Contains(strings.ToLower(title), strings.ToLower("("+original+")")) {
		return ipadNormalizeTitle(title + " (" + original + ")")
	}
	return ipadNormalizeTitle(title)
}

func ipadSafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune("<>:|?*", r) || r == 34 || r == 47 || r == 92 {
			return '-'
		}
		return r
	}, s)
	s = strings.TrimSpace(strings.Trim(s, ". "))
	chars := []rune(s)
	if len(chars) > 110 {
		s = string(chars[:110])
	}
	return s
}

type ipadSavedJob struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Percent    int    `json:"percent"`
	FileName   string `json:"fileName"`
	Size       int64  `json:"size"`
	Error      string `json:"error"`
	Source     string `json:"source"`
	Output     string `json:"output"`
	Profile    string `json:"profile,omitempty"`
	SourceSize int64  `json:"sourceSize,omitempty"`
}
type ipadSavedQueue struct {
	Jobs  []ipadSavedJob `json:"jobs"`
	Queue []string       `json:"queue"`
}

func ipadQueueFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ipad-queue.json"), nil
}

func (s *Server) ipadSaveLocked() error {
	path, err := ipadQueueFile()
	if err != nil {
		return err
	}
	result := ipadSavedQueue{Queue: append([]string(nil), s.ipadQueue...)}
	keys := make([]string, 0, len(s.ipadJobs))
	for k := range s.ipadJobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		j := s.ipadJobs[id]
		result.Jobs = append(result.Jobs, ipadSavedJob{
			ID: j.ID, Status: j.Status, Percent: j.Percent, FileName: j.FileName,
			Size: j.Size, Error: j.Error, Source: j.Source, Output: j.Output, Profile: j.Profile, SourceSize: j.SourceSize,
		})
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func ipadConcurrency() int {
	n, _ := strconv.Atoi(os.Getenv("KINOPUB_IPAD_CONCURRENT"))
	if n == 1 {
		return 1
	}
	return 2
}
func (s *Server) ipadStartNextLocked() {
	if s.ipadActive == nil {
		s.ipadActive = make(map[string]struct{})
	}
	for len(s.ipadActive) < ipadConcurrency() && len(s.ipadQueue) > 0 {
		id := s.ipadQueue[0]
		s.ipadQueue = s.ipadQueue[1:]
		j := s.ipadJobs[id]
		if j == nil || j.Status != "queued" {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		j.Status = "running"
		j.Cancel = cancel
		j.Percent = 0
		s.ipadActive[id] = struct{}{}
		_ = s.ipadSaveLocked()
		go s.runIPadExport(ctx, j)
	}
	_ = s.ipadSaveLocked()
}

func (s *Server) restoreIPadQueue() {
	path, err := ipadQueueFile()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved ipadSavedQueue
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	s.ipadMu.Lock()
	defer s.ipadMu.Unlock()
	s.ipadJobs = make(map[string]*ipadJob)
	for _, v := range saved.Jobs {
		if len(v.ID) != 32 || v.Source == "" || v.Output == "" {
			continue
		}
		real, info, e := s.localMediaPath(v.Source)
		if e != nil {
			continue
		}
		profile := v.Profile
		if profile == "" {
			profile = "fast"
		} // migration of pre-profile exports
		if profile != "fast" && profile != "compact" && profile != "balanced" {
			continue
		}
		id, _, e := ipadOutputForProfile(real, info, profile)
		if e != nil || id != v.ID {
			continue
		}
		root, e := ipadExportsRoot()
		if e != nil || !pathInRoot(filepath.Clean(v.Output), filepath.Clean(root)) {
			continue
		}
		j := &ipadJob{ID: id, Source: real, Output: v.Output, Status: v.Status,
			Percent: v.Percent, FileName: v.FileName, Size: v.Size, Error: v.Error, Profile: profile, SourceSize: info.Size()}
		if st, e := os.Stat(j.Output); e == nil && st.Mode().IsRegular() && st.Size() > 0 {
			j.Status = "ready"
			j.Percent = 100
			j.Size = st.Size()
			j.Error = ""
		} else if j.Status == "ready" {
			j.Status = "failed"
			j.Error = "Готовый файл отсутствует"
		} else if j.Status == "running" || j.Status == "queued" {
			j.Status = "queued"
			j.Percent = 0
			j.Error = ""
		}
		s.ipadJobs[id] = j
	}
	inQueue := make(map[string]bool)
	for _, id := range saved.Queue {
		j := s.ipadJobs[id]
		if j != nil && j.Status == "queued" && !inQueue[id] {
			s.ipadQueue = append(s.ipadQueue, id)
			inQueue[id] = true
		}
	}
	for _, j := range saved.Jobs {
		if restored := s.ipadJobs[j.ID]; restored != nil && restored.Status == "queued" && !inQueue[j.ID] {
			s.ipadQueue = append(s.ipadQueue, j.ID)
			inQueue[j.ID] = true
		}
	}
	s.ipadStartNextLocked()
}

func (s *Server) ipadViewLocked(j *ipadJob) ipadJob {
	v := *j
	v.SourcePath = j.Source
	if v.Profile == "" {
		v.Profile = "fast"
	}
	if j.Status == "queued" {
		for i, id := range s.ipadQueue {
			if j.ID == id {
				v.Position = i + 1
				break
			}
		}
	}
	return v
}

// Only obsolete queue records are removed. Video files remain untouched.
func (s *Server) ipadPruneHistoryLocked(removeFailures bool) int {
	n := 0
	for id, j := range s.ipadJobs {
		if j.Status == "running" || j.Status == "queued" {
			continue
		}
		stale := j.Status == "deleted" || j.Status == "canceled" ||
			(j.Status == "failed" && j.Error == "Готовый файл отсутствует")
		if j.Status == "ready" {
			st, err := os.Stat(j.Output)
			stale = err != nil || !st.Mode().IsRegular() || st.Size() == 0
		}
		if removeFailures && j.Status == "failed" {
			stale = true
		}
		if stale {
			delete(s.ipadJobs, id)
			n++
		}
	}
	if n > 0 {
		_ = s.ipadSaveLocked()
	}
	return n
}

func (s *Server) handleIPadClear(w http.ResponseWriter, r *http.Request) {
	s.ipadMu.Lock()
	removed := s.ipadPruneHistoryLocked(true)
	s.ipadMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

func (s *Server) handleIPadQueue(w http.ResponseWriter, r *http.Request) {
	s.ipadMu.Lock()
	s.ipadPruneHistoryLocked(false)
	queue := make([]ipadJob, 0, len(s.ipadJobs))
	var running []*ipadJob
	for id := range s.ipadActive {
		if j := s.ipadJobs[id]; j != nil {
			running = append(running, j)
		}
	}
	sort.Slice(running, func(i, j int) bool { return running[i].FileName < running[j].FileName })
	for _, j := range running {
		queue = append(queue, s.ipadViewLocked(j))
	}
	for _, id := range s.ipadQueue {
		if j := s.ipadJobs[id]; j != nil {
			queue = append(queue, s.ipadViewLocked(j))
		}
	}
	var done []ipadJob
	for _, j := range s.ipadJobs {
		if _, active := s.ipadActive[j.ID]; !active && j.Status != "queued" {
			done = append(done, s.ipadViewLocked(j))
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].FileName < done[j].FileName })
	queue = append(queue, done...)
	s.ipadMu.Unlock()
	writeJSON(w, http.StatusOK, queue)
}
