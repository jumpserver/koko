package httpd

import (
	"context"
	"errors"
	"testing"
)

type recordingFileAILifecycle struct {
	events *[]string
}

func (s recordingFileAILifecycle) Cancel() {
	*s.events = append(*s.events, "cancel")
}

func (s recordingFileAILifecycle) Close() {
	*s.events = append(*s.events, "close")
}

func TestWebSFTPFileExecutorPermissionGates(t *testing.T) {
	denied := func() bool { return false }
	executor := &webSFTPFileExecutor{
		canDownload: denied,
		canUpload:   denied,
		canDelete:   denied,
	}
	ctx := context.Background()
	checks := []func() error{
		func() error {
			_, err := executor.ReadText(ctx, "/file", 1)
			return err
		},
		func() error {
			_, err := executor.SaveText(ctx, "/file", "x", "v1")
			return err
		},
		func() error { return executor.Mkdir(ctx, "/dir") },
		func() error { return executor.Rename(ctx, "/old", "/new") },
		func() error { return executor.Delete(ctx, "/file") },
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Fatal("denied file permission was accepted")
		}
	}
}

func TestWebSFTPFileExecutorRejectsOutOfScopePaths(t *testing.T) {
	scopeErr := errors.New("outside SFTP root")
	executor := &webSFTPFileExecutor{
		validatePath: func(path string) error {
			if path == "/root/x.txt" {
				return scopeErr
			}
			return nil
		},
	}
	ctx := context.Background()
	checks := []func() error{
		func() error {
			_, err := executor.ListDirectory(ctx, "/root/x.txt", 1)
			return err
		},
		func() error {
			_, err := executor.Stat(ctx, "/root/x.txt")
			return err
		},
		func() error {
			_, err := executor.ReadText(ctx, "/root/x.txt", 1)
			return err
		},
		func() error {
			_, err := executor.SaveText(ctx, "/root/x.txt", "x", "absent")
			return err
		},
		func() error { return executor.Mkdir(ctx, "/root/x.txt") },
		func() error { return executor.Rename(ctx, "/tmp/x.txt", "/root/x.txt") },
		func() error { return executor.Delete(ctx, "/root/x.txt") },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, scopeErr) {
			t.Fatalf("out-of-scope operation error = %v", err)
		}
	}
}

func TestCloseWebSFTPResourcesOrder(t *testing.T) {
	events := make([]string, 0, 3)
	closeWebSFTPResources(
		recordingFileAILifecycle{events: &events},
		func() { events = append(events, "volume") },
	)
	want := []string{"cancel", "volume", "close"}
	if len(events) != len(want) {
		t.Fatalf("events = %v", events)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("events = %v", events)
		}
	}
}
