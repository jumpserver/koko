package webproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	recordingPathPrefix = "/_jumpserver/web-recordings"
	maxFrameSize        = 2 << 20
	maxRecordingTimeMS  = int64((24 * time.Hour) / time.Millisecond)
)

type recordingManager struct {
	mu         sync.Mutex
	root       string
	ffmpegPath string
	sessions   map[string]*webRecording
	encode     func(context.Context, *webRecording) error
}

type webRecording struct {
	mu            sync.Mutex
	id            string
	dir           string
	targetURL     string
	width         int
	height        int
	startedAt     time.Time
	frames        []recordedFrame
	lastHash      [sha256.Size]byte
	hasLastHash   bool
	lastTimestamp int64
	closed        bool
}

type recordedFrame struct {
	Name        string `json:"name"`
	TimestampMS int64  `json:"timestamp_ms"`
}

type recordingMetadata struct {
	ID         string          `json:"id"`
	TargetURL  string          `json:"target_url,omitempty"`
	Width      int             `json:"width"`
	Height     int             `json:"height"`
	StartedAt  time.Time       `json:"started_at"`
	DurationMS int64           `json:"duration_ms"`
	Frames     []recordedFrame `json:"frames"`
}

type recordingResult struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	FrameCount int    `json:"frame_count"`
	DurationMS int64  `json:"duration_ms"`
}

func newRecordingManager(root, ffmpegPath string) (*recordingManager, error) {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create Web recording root: %w", err)
	}
	manager := &recordingManager{
		root:       root,
		ffmpegPath: ffmpegPath,
		sessions:   make(map[string]*webRecording),
	}
	manager.encode = manager.encodeMP4
	return manager, nil
}

func (m *recordingManager) start(targetURL string, width, height int) (*webRecording, error) {
	id, err := randomRecordingID()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(m.root, id)
	if err = os.MkdirAll(filepath.Join(dir, "frames"), 0o700); err != nil {
		return nil, fmt.Errorf("create Web recording directory: %w", err)
	}
	recording := &webRecording{
		id:        id,
		dir:       dir,
		targetURL: sanitizedRecordingURL(targetURL),
		width:     width,
		height:    height,
		startedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	m.sessions[id] = recording
	m.mu.Unlock()
	return recording, nil
}

func (m *recordingManager) frame(id string, timestampMS int64, jpeg []byte) (bool, int, error) {
	m.mu.Lock()
	recording := m.sessions[id]
	m.mu.Unlock()
	if recording == nil {
		return false, 0, errors.New("recording not found")
	}
	return recording.addFrame(timestampMS, jpeg)
}

func (m *recordingManager) finish(ctx context.Context, id string, durationMS int64) (recordingResult, error) {
	m.mu.Lock()
	recording := m.sessions[id]
	if recording != nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if recording == nil {
		return recordingResult{}, errors.New("recording not found")
	}

	recording.mu.Lock()
	recording.closed = true
	if durationMS < recording.lastTimestamp {
		durationMS = recording.lastTimestamp
	}
	recording.lastTimestamp = durationMS
	metadata := recordingMetadata{
		ID:         recording.id,
		TargetURL:  recording.targetURL,
		Width:      recording.width,
		Height:     recording.height,
		StartedAt:  recording.startedAt,
		DurationMS: durationMS,
		Frames:     append([]recordedFrame(nil), recording.frames...),
	}
	recording.mu.Unlock()
	if len(metadata.Frames) == 0 {
		_ = os.RemoveAll(recording.dir)
		return recordingResult{}, errors.New("recording has no frames")
	}

	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return recordingResult{}, err
	}
	if err = os.WriteFile(filepath.Join(recording.dir, "recording.json"), metadataJSON, 0o600); err != nil {
		return recordingResult{}, fmt.Errorf("write Web recording metadata: %w", err)
	}
	if err = m.encode(ctx, recording); err != nil {
		return recordingResult{}, err
	}
	_ = os.RemoveAll(filepath.Join(recording.dir, "frames"))
	_ = os.Remove(filepath.Join(recording.dir, "frames.ffconcat"))

	return recordingResult{
		ID:         id,
		Path:       filepath.Join(recording.dir, "recording.mp4"),
		FrameCount: len(metadata.Frames),
		DurationMS: durationMS,
	}, nil
}

func (m *recordingManager) cancel(id string) error {
	m.mu.Lock()
	recording := m.sessions[id]
	if recording != nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if recording == nil {
		return errors.New("recording not found")
	}
	recording.mu.Lock()
	recording.closed = true
	recording.mu.Unlock()
	return os.RemoveAll(recording.dir)
}

func (r *webRecording) addFrame(timestampMS int64, frameData []byte) (bool, int, error) {
	if timestampMS < 0 || timestampMS > maxRecordingTimeMS {
		return false, 0, errors.New("invalid frame timestamp")
	}
	if len(frameData) < 4 || frameData[0] != 0xff || frameData[1] != 0xd8 || frameData[len(frameData)-2] != 0xff || frameData[len(frameData)-1] != 0xd9 {
		return false, 0, errors.New("invalid JPEG frame")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false, len(r.frames), errors.New("recording is closed")
	}
	if timestampMS < r.lastTimestamp {
		return false, len(r.frames), errors.New("frame timestamps must be monotonic")
	}
	r.lastTimestamp = timestampMS
	hash := sha256.Sum256(frameData)
	if r.hasLastHash && hash == r.lastHash {
		return false, len(r.frames), nil
	}

	name := fmt.Sprintf("%08d.jpg", len(r.frames)+1)
	if len(r.frames) == 0 {
		if dimensions, err := jpeg.DecodeConfig(bytes.NewReader(frameData)); err == nil {
			r.width = dimensions.Width
			r.height = dimensions.Height
		}
	}
	if err := os.WriteFile(filepath.Join(r.dir, "frames", name), frameData, 0o600); err != nil {
		return false, len(r.frames), fmt.Errorf("write Web recording frame: %w", err)
	}
	r.frames = append(r.frames, recordedFrame{Name: name, TimestampMS: timestampMS})
	r.lastHash = hash
	r.hasLastHash = true
	return true, len(r.frames), nil
}

func (m *recordingManager) encodeMP4(ctx context.Context, recording *webRecording) error {
	recording.mu.Lock()
	frames := append([]recordedFrame(nil), recording.frames...)
	durationMS := recording.lastTimestamp
	recording.mu.Unlock()

	var concat strings.Builder
	for index, frame := range frames {
		frameDurationMS := durationMS - frame.TimestampMS
		if index+1 < len(frames) {
			frameDurationMS = frames[index+1].TimestampMS - frame.TimestampMS
		}
		if frameDurationMS < 100 {
			frameDurationMS = 100
		}
		fmt.Fprintf(&concat, "file 'frames/%s'\nduration %.3f\n", frame.Name, float64(frameDurationMS)/1000)
	}
	fmt.Fprintf(&concat, "file 'frames/%s'\n", frames[len(frames)-1].Name)
	if err := os.WriteFile(filepath.Join(recording.dir, "frames.ffconcat"), []byte(concat.String()), 0o600); err != nil {
		return fmt.Errorf("write Web recording timeline: %w", err)
	}

	encodeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(
		encodeCtx,
		m.ffmpegPath,
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", "frames.ffconcat",
		"-an",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"recording.mp4",
	)
	cmd.Dir = recording.dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 4096 {
			output = output[len(output)-4096:]
		}
		return fmt.Errorf("generate Web recording MP4: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Server) serveRecording(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !isLoopbackRemote(r.RemoteAddr) {
		http.Error(w, "recording control API is loopback-only", http.StatusForbidden)
		return
	}
	if s.recordings == nil {
		http.Error(w, "Web recording is disabled", http.StatusNotFound)
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, recordingPathPrefix), "/")
	parts := strings.Split(path, "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		s.startRecording(w, r)
	case len(parts) == 2 && parts[1] == "frames" && r.Method == http.MethodPost:
		s.addRecordingFrame(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "finish" && r.Method == http.MethodPost:
		s.finishRecording(w, r, parts[0])
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.cancelRecording(w, parts[0])
	default:
		http.Error(w, "recording endpoint not found", http.StatusNotFound)
	}
}

func (s *Server) cancelRecording(w http.ResponseWriter, id string) {
	if err := s.recordings.cancel(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startRecording(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TargetURL string `json:"target_url"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	}
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if request.Width < 1 || request.Width > 8192 || request.Height < 1 || request.Height > 8192 {
		http.Error(w, "invalid recording dimensions", http.StatusBadRequest)
		return
	}
	recording, err := s.recordings.start(request.TargetURL, request.Width, request.Height)
	if err != nil {
		http.Error(w, "unable to start recording", http.StatusInternalServerError)
		return
	}
	logger.Infof("Web recording %s started for %s", recording.id, recording.targetURL)
	writeRecordingJSON(w, http.StatusCreated, map[string]string{"id": recording.id})
}

func (s *Server) addRecordingFrame(w http.ResponseWriter, r *http.Request, id string) {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "image/jpeg" {
		http.Error(w, "recording frames must use image/jpeg", http.StatusUnsupportedMediaType)
		return
	}
	timestampMS, err := strconv.ParseInt(r.URL.Query().Get("timestamp_ms"), 10, 64)
	if err != nil {
		http.Error(w, "invalid frame timestamp", http.StatusBadRequest)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxFrameSize)
	jpeg, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "frame exceeds size limit", http.StatusRequestEntityTooLarge)
		return
	}
	accepted, frameCount, err := s.recordings.frame(id, timestampMS, jpeg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeRecordingJSON(w, http.StatusOK, map[string]any{"accepted": accepted, "frame_count": frameCount})
}

func (s *Server) finishRecording(w http.ResponseWriter, r *http.Request, id string) {
	var request struct {
		DurationMS int64 `json:"duration_ms"`
	}
	if err := decodeLimitedJSON(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if request.DurationMS < 0 || request.DurationMS > maxRecordingTimeMS {
		http.Error(w, "invalid recording duration", http.StatusBadRequest)
		return
	}
	result, err := s.recordings.finish(r.Context(), id, request.DurationMS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Infof("Web recording %s generated at %s (%d frames, %d ms)", result.ID, result.Path, result.FrameCount, result.DurationMS)
	writeRecordingJSON(w, http.StatusOK, result)
}

func decodeLimitedJSON(w http.ResponseWriter, r *http.Request, value any) error {
	body := http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid JSON request: %w", err)
	}
	return nil
}

func writeRecordingJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomRecordingID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate recording ID: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func sanitizedRecordingURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
