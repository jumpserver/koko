package httpd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type volumeTestFS struct {
	sftpFileSystem
	files    fstest.MapFS
	mutation string
	onStat   func()
}

func (f *volumeTestFS) Stat(path string) (os.FileInfo, error) {
	if f.onStat != nil {
		f.onStat()
	}
	return f.files.Stat(strings.TrimPrefix(path, "/"))
}

func (f *volumeTestFS) Lstat(path string) (os.FileInfo, error) {
	return f.files.Lstat(strings.TrimPrefix(path, "/"))
}

func (f *volumeTestFS) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }

func (f *volumeTestFS) ResolveAgentToolPath(path string) (string, error) {
	if path == "/link" {
		return "/root/source", nil
	}
	return filepath.Join("/root", path), nil
}

func (f *volumeTestFS) MkdirExact(path string) error {
	f.mutation = "mkdir " + path
	return nil
}

func (f *volumeTestFS) Rename(source, destination string) error {
	f.mutation = "rename " + source + " " + destination
	return nil
}

func (f *volumeTestFS) Remove(path string) error {
	f.mutation = "remove " + path
	return nil
}

func (f *volumeTestFS) RemoveDirectory(path string) error {
	f.mutation = "rmdir " + path
	return nil
}

func TestSFTPVolumeMakeDir(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		files      fstest.MapFS
		wantErr    error
	}{
		{name: "new", path: "/new"},
		{name: "existing", path: "/new", files: fstest.MapFS{"root/new": {}}, wantErr: os.ErrExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &volumeTestFS{files: tc.files}
			err := newSFTPVolume(fs, nil).MakeDir(context.Background(), tc.path, fileMutationOptions{confined: true})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("MakeDir error = %v, want %v", err, tc.wantErr)
			}
			want := ""
			if tc.wantErr == nil {
				want = "mkdir /root/new"
			}
			if fs.mutation != want {
				t.Fatalf("mutation = %q, want %q", fs.mutation, want)
			}
		})
	}
}

func TestSFTPVolumeRename(t *testing.T) {
	for _, name := range []string{"without version", "version matches", "version changed", "destination exists"} {
		t.Run(name, func(t *testing.T) {
			fs := &volumeTestFS{files: fstest.MapFS{"root/source": {Data: []byte("abc"), Mode: 0644}}}
			info, _ := fs.Stat("/root/source")
			version := webSftpFileVersion(info)
			options := fileMutationOptions{confined: true, expectedVersion: &version}
			var wantErr error
			switch name {
			case "without version":
				options.expectedVersion = nil
			case "version changed":
				fs.files["root/source"].Data = []byte("changed")
				wantErr = ErrWebSftpFileConflict
			case "destination exists":
				fs.files["root/destination"] = &fstest.MapFile{}
				wantErr = os.ErrExist
			}
			err := newSFTPVolume(fs, nil).Rename(context.Background(), "/source", "/destination", options)
			if !errors.Is(err, wantErr) {
				t.Fatalf("Rename error = %v, want %v", err, wantErr)
			}
			want := ""
			if wantErr == nil {
				want = "rename /root/source /root/destination"
			}
			if fs.mutation != want {
				t.Fatalf("mutation = %q, want %q", fs.mutation, want)
			}
		})
	}
}

func TestSFTPVolumeRemove(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
		mode             os.FileMode
		recursive, stale bool
		wantError        bool
	}{
		{name: "file", path: "/source", want: "remove /root/source"},
		{name: "changed", path: "/source", stale: true, wantError: true},
		{name: "directory requires recursive", path: "/source", mode: os.ModeDir, wantError: true},
		{name: "recursive directory", path: "/source", mode: os.ModeDir, recursive: true, want: "rmdir /root/source"},
		{name: "symlink to directory", path: "/link", mode: os.ModeDir, want: "remove /root/link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &volumeTestFS{files: fstest.MapFS{
				"root/source": {Mode: tc.mode | 0755},
				"root/link":   {Data: []byte("source"), Mode: os.ModeSymlink | 0777},
			}}
			info, _ := fs.Stat("/root/source")
			version := webSftpFileVersion(info)
			if tc.stale {
				fs.files["root/source"].Mode = 0600
			}
			err := newSFTPVolume(fs, nil).Remove(context.Background(), tc.path, tc.recursive,
				fileMutationOptions{confined: true, expectedVersion: &version})
			if (err != nil) != tc.wantError || (tc.stale && !errors.Is(err, ErrWebSftpFileConflict)) {
				t.Fatalf("unexpected Remove error: %v", err)
			}
			if fs.mutation != tc.want {
				t.Fatalf("mutation = %q, want %q", fs.mutation, tc.want)
			}
		})
	}
}

func TestSFTPVolumeCanceledMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Any remote call panics: cancellation must be checked before touching I/O.
	u := newSFTPVolume(nil, nil)
	if err := u.MakeDir(ctx, "/new", fileMutationOptions{confined: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("mutation error = %v, want context.Canceled", err)
	}
}

func TestSFTPVolumeCanceledDuringPreflight(t *testing.T) {
	for _, name := range []string{"mkdir", "rename", "remove"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fs := &volumeTestFS{files: fstest.MapFS{"source": {Mode: 0644}}}
			info, _ := fs.Stat("/source")
			version := webSftpFileVersion(info)
			fs.onStat = cancel
			u := newSFTPVolume(fs, nil)
			var err error
			switch name {
			case "mkdir":
				err = u.MakeDir(ctx, "/new", fileMutationOptions{})
			case "rename":
				err = u.Rename(ctx, "/source", "/destination", fileMutationOptions{})
			case "remove":
				err = u.Remove(ctx, "/source", false, fileMutationOptions{expectedVersion: &version})
			}
			if !errors.Is(err, context.Canceled) || fs.mutation != "" {
				t.Fatalf("canceled preflight mutated filesystem: error=%v, mutation=%q", err, fs.mutation)
			}
		})
	}
}

func TestSFTPVolumeDirectoryDownload(t *testing.T) {
	file, _, err := newSFTPVolume(&volumeTestFS{}, nil).Download("/empty", true)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	archivePath := file.(*temporaryDownload).Name()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("invalid ZIP archive: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive remains after Close: %v", err)
	}
}

func TestSFTPFileInfoWireMetadata(t *testing.T) {
	files := fstest.MapFS{"file": {Data: []byte("abc"), Mode: 0644, ModTime: time.Unix(1787816033, 0)}}
	info, _ := files.Stat("file")
	entry := newWebSftpFileInfo(info)
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Size    string `json:"size"`
		ModTime string `json:"mod_time"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Size != "3" || wire.ModTime != "1787816033" {
		t.Fatalf("metadata changed across wire encoding: %s", data)
	}
}
