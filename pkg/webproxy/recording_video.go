package webproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	maxVideoChunkSize = 8 << 20
	maxVideoBytes     = int64(4 << 30)
)

var (
	videoUploadSlots = make(chan struct{}, 8)
	videoEncodeSlots = make(chan struct{}, 2)
)

func (s *Server) addRecordingVideoChunk(w http.ResponseWriter, r *http.Request, id string) {
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		http.Error(w, "video chunks must use application/octet-stream", http.StatusUnsupportedMediaType)
		return
	}
	sequence, err := strconv.Atoi(r.URL.Query().Get("sequence"))
	if err != nil || sequence < 0 {
		http.Error(w, "invalid video chunk sequence", http.StatusBadRequest)
		return
	}
	select {
	case videoUploadSlots <- struct{}{}:
		defer func() { <-videoUploadSlots }()
	default:
		http.Error(w, "video upload is busy", http.StatusServiceUnavailable)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVideoChunkSize))
	if err != nil {
		http.Error(w, "video chunk exceeds size limit", http.StatusRequestEntityTooLarge)
		return
	}
	s.recordings.mu.Lock()
	recording := s.recordings.sessions[id]
	s.recordings.mu.Unlock()
	if recording == nil {
		http.NotFound(w, r)
		return
	}
	count, err := recording.addVideoChunk(sequence, body, r.URL.Query().Get("resized") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeRecordingJSON(w, http.StatusOK, map[string]int{"chunk_count": count})
}

func (r *webRecording) addVideoChunk(sequence int, data []byte, resized bool) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || (r.format != "mp4" && r.format != "webm") {
		return 0, errors.New("video recording is closed or unavailable")
	}
	if len(data) == 0 || len(data) > maxVideoChunkSize || time.Since(r.startedAt) > 24*time.Hour {
		return 0, errors.New("video chunk exceeds recording limits")
	}
	hash := sha256.Sum256(data)
	// A lost HTTP response may cause the most recent chunk to be retried.
	if sequence == r.chunkCount-1 && hash == r.lastChunkHash {
		return r.chunkCount, nil
	}
	if sequence != r.chunkCount || r.videoBytes+int64(len(data)) > maxVideoBytes {
		return 0, errors.New("unexpected video chunk sequence or recording size limit")
	}
	if sequence == 0 {
		valid := r.format == "mp4" && len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp"))
		valid = valid || (r.format == "webm" && bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}))
		if !valid {
			return 0, errors.New("invalid video container header")
		}
	}
	file, err := os.OpenFile(filepath.Join(r.dir, "video."+r.format), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	_, err = file.WriteAt(data, r.videoBytes)
	if err != nil {
		_ = file.Truncate(r.videoBytes)
	}
	closeErr := file.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	r.videoBytes += int64(len(data))
	r.chunkCount++
	r.lastChunkHash = hash
	r.resized = r.resized || resized
	return r.chunkCount, nil
}

// Keep FFmpeg diagnostics bounded even for a long or damaged input stream.
type recordingErrorBuffer struct{ data []byte }

func (b *recordingErrorBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > 4096 {
		p = p[len(p)-4096:]
	}
	if len(b.data)+len(p) > 4096 {
		b.data = b.data[len(b.data)+len(p)-4096:]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (m *recordingManager) encodeVideoMP4(ctx context.Context, recording *webRecording) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	select {
	case videoEncodeSlots <- struct{}{}:
		defer func() { <-videoEncodeSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-threads", "2", "-max_pixels", "2073600", "-i", "video." + recording.format,
		"-map", "0:v:0", "-an", "-map_metadata", "-1"}
	if recording.format == "mp4" && !recording.resized {
		// Chromium's avc3 recording can be remuxed without decoding and re-encoding.
		args = append(args, "-c:v", "copy")
	} else {
		width, height := max(2, recording.width/2*2), max(2, recording.height/2*2)
		filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1", width, height, width, height)
		args = append(args, "-filter_threads", "1", "-vf", filter,
			"-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p")
	}
	args = append(args, "-movflags", "+faststart", "recording.mp4")
	cmd := exec.CommandContext(ctx, m.ffmpegPath, args...)
	cmd.Dir = recording.dir
	var diagnostics recordingErrorBuffer
	cmd.Stderr = &diagnostics
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("finalize Web video recording: %w: %s", err, diagnostics.data)
	}
	return nil
}
