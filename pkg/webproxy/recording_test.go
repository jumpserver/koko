package webproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWebRecordingLifecycle(t *testing.T) {
	proxy, err := NewServer("127.0.0.1", "0", "*", t.TempDir(), "ffmpeg", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy.recordings.encode = func(_ context.Context, recording *webRecording) error {
		return os.WriteFile(filepath.Join(recording.dir, "recording.mp4"), []byte("video"), 0o600)
	}

	startResponse := performRecordingRequest(t, proxy, http.MethodPost, recordingPathPrefix,
		[]byte(`{"target_url":"https://example.com/path?token=secret#fragment","width":1280,"height":720}`))
	if startResponse.Code != http.StatusCreated {
		t.Fatalf("unexpected start status %d: %s", startResponse.Code, startResponse.Body.String())
	}
	var started map[string]string
	if err = json.Unmarshal(startResponse.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	id := started["id"]
	if id == "" {
		t.Fatal("missing recording ID")
	}

	firstJPEG := []byte{0xff, 0xd8, 0xff, 0x01, 0xff, 0xd9}
	framePath := recordingPathPrefix + "/" + id + "/frames?timestamp_ms=0"
	frameResponse := performRecordingRequest(t, proxy, http.MethodPost, framePath, firstJPEG)
	if frameResponse.Code != http.StatusOK {
		t.Fatalf("unexpected frame status %d: %s", frameResponse.Code, frameResponse.Body.String())
	}

	duplicatePath := recordingPathPrefix + "/" + id + "/frames?timestamp_ms=500"
	duplicateResponse := performRecordingRequest(t, proxy, http.MethodPost, duplicatePath, firstJPEG)
	var duplicateResult struct {
		Accepted   bool `json:"accepted"`
		FrameCount int  `json:"frame_count"`
	}
	if err = json.Unmarshal(duplicateResponse.Body.Bytes(), &duplicateResult); err != nil {
		t.Fatal(err)
	}
	if duplicateResult.Accepted || duplicateResult.FrameCount != 1 {
		t.Fatalf("expected duplicate frame to be discarded: %+v", duplicateResult)
	}

	secondJPEG := []byte{0xff, 0xd8, 0xff, 0x02, 0xff, 0xd9}
	secondPath := recordingPathPrefix + "/" + id + "/frames?timestamp_ms=1000"
	if response := performRecordingRequest(t, proxy, http.MethodPost, secondPath, secondJPEG); response.Code != http.StatusOK {
		t.Fatalf("unexpected second frame status %d: %s", response.Code, response.Body.String())
	}

	finishPath := recordingPathPrefix + "/" + id + "/finish"
	finishResponse := performRecordingRequest(t, proxy, http.MethodPost, finishPath, []byte(`{"duration_ms":1500}`))
	if finishResponse.Code != http.StatusOK {
		t.Fatalf("unexpected finish status %d: %s", finishResponse.Code, finishResponse.Body.String())
	}
	var result recordingResult
	if err = json.Unmarshal(finishResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.FrameCount != 2 || result.DurationMS != 1500 {
		t.Fatalf("unexpected recording result: %+v", result)
	}
	if _, err = os.Stat(result.Path); err != nil {
		t.Fatalf("recording output not found: %v", err)
	}
	metadata, err := os.ReadFile(filepath.Join(filepath.Dir(result.Path), "recording.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(metadata, []byte("secret")) || !bytes.Contains(metadata, []byte("https://example.com/path")) {
		t.Fatalf("target URL was not sanitized: %s", metadata)
	}
}

func TestWebRecordingCancellationRemovesPendingFrames(t *testing.T) {
	root := t.TempDir()
	manager, err := newRecordingManager(root, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	recording, err := manager.start("https://example.com", 32, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.cancel(recording.id); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(recording.dir); !os.IsNotExist(err) {
		t.Fatalf("pending recording was not removed: %v", err)
	}
}

func TestWebRecordingControlRejectsNonLoopback(t *testing.T) {
	proxy, err := NewServer("127.0.0.1", "0", "*", t.TempDir(), "ffmpeg", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, recordingPathPrefix, bytes.NewReader([]byte(`{}`)))
	request.RemoteAddr = "192.0.2.10:1234"
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected loopback-only API, got %d", response.Code)
	}
}

func TestProxyRequestCannotReachRecordingControlAPI(t *testing.T) {
	proxy, err := NewServer("127.0.0.1", "0", "example.com", t.TempDir(), "ffmpeg", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://blocked.invalid"+recordingPathPrefix, bytes.NewReader([]byte(`{}`)))
	request.RemoteAddr = "127.0.0.1:54321"
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected proxy allowlist rejection, got %d", response.Code)
	}
}

func TestWebRecordingGeneratesMP4(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	manager, err := newRecordingManager(t.TempDir(), ffmpegPath)
	if err != nil {
		t.Fatal(err)
	}
	recording, err := manager.start("https://example.com", 32, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.frame(recording.id, 0, solidJPEG(t, color.RGBA{R: 255, A: 255})); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.frame(recording.id, 500, solidJPEG(t, color.RGBA{B: 255, A: 255})); err != nil {
		t.Fatal(err)
	}
	result, err := manager.finish(context.Background(), recording.id, 1000)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("generated MP4 is empty")
	}
}

func performRecordingRequest(t *testing.T, handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:54321"
	if bytes.Contains([]byte(path), []byte("/frames")) {
		request.Header.Set("Content-Type", "image/jpeg")
	} else {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func solidJPEG(t *testing.T, fill color.Color) []byte {
	t.Helper()
	imageData := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < imageData.Bounds().Dy(); y++ {
		for x := 0; x < imageData.Bounds().Dx(); x++ {
			imageData.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, imageData, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
