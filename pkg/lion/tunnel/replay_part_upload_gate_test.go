package tunnel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service/videoworker"
	"github.com/jumpserver/koko/pkg/config"
)

func TestVideoWorkerDeliveryRequiresValidLicense(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		terminalCfg *model.TerminalConfig
		wantWorker  bool
	}{
		{name: "disabled by operator", terminalCfg: &model.TerminalConfig{LicenseIsValid: true}},
		{name: "missing terminal config", enabled: true},
		{name: "CE without license", enabled: true, terminalCfg: &model.TerminalConfig{}},
		{name: "expired license", enabled: true, terminalCfg: &model.TerminalConfig{LicenseContent: "expired"}},
		{name: "licensed EE", enabled: true, terminalCfg: &model.TerminalConfig{LicenseIsValid: true}, wantWorker: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseVideoWorker(tt.enabled, tt.terminalCfg); got != tt.wantWorker {
				t.Fatalf("shouldUseVideoWorker() = %t, want %t", got, tt.wantWorker)
			}

			root := t.TempDir()
			uploader := PartUploader{SessionId: "session-id", RootPath: root, TermCfg: tt.terminalCfg}
			workerCalls, originalCalls := 0, 0
			uploader.uploadToStorageWith(root, config.Config{EnableVideoWorker: tt.enabled},
				func(sessionID, uploadPath string, taskCfg *videoworker.TaskConfig) (string, error) {
					workerCalls++
					if sessionID != uploader.SessionId || uploadPath != root || taskCfg.Bitrate != 1 {
						t.Fatalf("unexpected worker task: session=%q upload=%q config=%+v", sessionID, uploadPath, taskCfg)
					}
					return "", errors.New("simulated worker outage")
				},
				func(uploadPath string) {
					originalCalls++
					if uploadPath != root {
						t.Fatalf("original storage path = %q, want %q", uploadPath, root)
					}
				})

			wantWorkerCalls := 0
			if tt.wantWorker {
				wantWorkerCalls = 1
			}
			if workerCalls != wantWorkerCalls {
				t.Fatalf("worker calls = %d, want %d", workerCalls, wantWorkerCalls)
			}
			if originalCalls != 1 {
				t.Fatalf("original storage calls = %d, want 1", originalCalls)
			}
		})
	}
}

func TestSuccessfulLicensedWorkerUploadDoesNotUseOriginalStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recording")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uploader := PartUploader{
		SessionId: "session-id",
		RootPath:  root,
		TermCfg:   &model.TerminalConfig{LicenseIsValid: true},
	}
	originalCalls := 0
	uploader.uploadToStorageWith(root, config.Config{EnableVideoWorker: true},
		func(string, string, *videoworker.TaskConfig) (string, error) { return "worker-task", nil },
		func(string) { originalCalls++ })
	if originalCalls != 0 {
		t.Fatalf("original storage calls = %d after successful worker submission", originalCalls)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source directory after successful worker upload: %v, want removed", err)
	}
}

func TestMissingTerminalConfigRetainsOriginalReplayForRecovery(t *testing.T) {
	root := t.TempDir()
	uploader := PartUploader{SessionId: "session-id", RootPath: root}
	uploader.uploadToOriginalStorage(root)
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("source directory was not retained for recovery: %v", err)
	}
}
