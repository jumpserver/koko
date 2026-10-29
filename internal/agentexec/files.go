package agentexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/assetexec"
)

type fileRequest struct {
	Path        string `json:"path"`
	Destination string `json:"destination"`
	Content     string `json:"content"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Overwrite   bool   `json:"overwrite"`
	Limit       int    `json:"limit"`
}

func (r *Runtime) artifactPath(id, suffix string) (string, error) {
	if !identifier.MatchString(id) {
		return "", errors.New("invalid file operation identifier")
	}
	return filepath.Join(r.opts.Directory, "files", id+suffix), nil
}

// reserveFile bounds total persistent transfer data, including interrupted
// transfers. The runtime lock also serializes reservations from concurrent jobs.
func (r *Runtime) reserveFile(id string, size int64) (*os.File, error) {
	if size < 0 || size > r.maxTransferBytes() {
		return nil, errors.New("file exceeds transfer limit")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(r.opts.Directory, "files"))
	if err != nil {
		return nil, err
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > (1<<30)-total {
			return nil, errors.New("invalid file staging entry or capacity reached")
		}
		total += info.Size()
	}
	if len(entries) >= 128 || total+size > 1<<30 {
		return nil, errors.New("file staging capacity reached")
	}
	path, err := r.artifactPath(id, ".part")
	if err != nil {
		return nil, err
	}
	final, _ := r.artifactPath(id, ".data")
	if _, err = os.Lstat(final); err == nil {
		return nil, os.ErrExist
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// Sparse logical length is the reservation, so concurrent streams cannot
	// reserve the same unfilled space. Copying never grows beyond this length.
	if err = f.Truncate(size); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return f, nil
}

func (r *Runtime) maxTransferBytes() int64 { return min(r.opts.MaxFileBytes, 100<<20) }

// persistArtifact publishes without replacement and retains the data only
// after the journal names it durably. Every failure removes this attempt.
func (r *Runtime) persistArtifact(id string, f *os.File, size int64, digest string) (err error) {
	final, err := r.artifactPath(id, ".data")
	if err != nil {
		return err
	}
	published, committed := false, false
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
		if published && !committed {
			_ = os.Remove(final)
			_ = syncDirectory(filepath.Dir(final))
		}
	}()
	if size < 0 || size > r.maxTransferBytes() {
		return errors.New("invalid artifact length")
	}
	if err = f.Truncate(size); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("artifact length changed")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link is atomic and fails if final already exists. Rename would overwrite
	// a previous artifact when requests with the same key arrive concurrently.
	if err = os.Link(f.Name(), final); err != nil {
		return err
	}
	published = true
	if err = os.Remove(f.Name()); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(final)); err != nil {
		return err
	}
	if err = r.journal.update(id, func(record *record) error {
		if record.Artifact != "" || (record.Action.Kind == "file_upload" && record.Dispatched) || terminalState(record.Action.Status) {
			return errors.New("operation already has file evidence or was dispatched")
		}
		record.Artifact, record.Size, record.SHA256 = final, size, digest
		return nil
	}); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Runtime) Upload(ctx context.Context, p Principal, id string, body io.Reader, length int64, digest string) (Action, error) {
	action, err := r.Get(ctx, p, id, 0, 1)
	if err != nil {
		return action, err
	}
	if action.Kind != "file_upload" {
		return action, errors.New("operation is not a file upload")
	}
	if terminalState(action.Status) {
		return action, nil
	}
	rec, err := r.journal.Action(id)
	if err != nil {
		return action, err
	}
	var req fileRequest
	if err = decodeRequest(rec.Action.Request, &req); err != nil {
		return action, err
	}
	if length < 0 || length != req.Size || digest != req.SHA256 || len(digest) != 64 {
		return action, errors.New("upload content does not match the approved size and digest")
	}
	if rec.Dispatched {
		return rec.Action, nil
	}
	s, err := r.getSession(p, rec.Action.SessionID, true)
	if err != nil {
		return action, err
	}
	if rec.Artifact != "" {
		expected, _ := r.artifactPath(id, ".data")
		if rec.Artifact != expected || rec.Size != req.Size || rec.SHA256 != req.SHA256 {
			return action, errors.New("upload evidence does not match the approved operation")
		}
		return action, r.enqueue(rec.Action, s)
	}
	s.mu.Lock()
	info := s.info
	s.mu.Unlock()
	action, err = r.authorize(ctx, rec.Action, info)
	if err != nil {
		return action, err
	}
	if action.Status != "queued" {
		return action, errors.New("upload is not approved and ready")
	}
	f, err := r.reserveFile(id, req.Size)
	if err != nil {
		return action, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	hash := sha256.New()
	if closer, ok := body.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	n, err := io.CopyBuffer(&boundedWriter{writer: io.MultiWriter(f, hash), remaining: req.Size}, io.LimitReader(body, req.Size+1), make([]byte, 32<<10))
	if err != nil {
		return action, err
	}
	if n != req.Size || hex.EncodeToString(hash.Sum(nil)) != req.SHA256 {
		return action, errors.New("upload checksum or length mismatch")
	}
	if err = ctx.Err(); err != nil {
		return action, err
	}
	if err = r.persistArtifact(id, f, n, req.SHA256); err != nil {
		return action, err
	}
	return action, r.enqueue(rec.Action, s)
}

func (r *Runtime) Download(ctx context.Context, p Principal, id string) (*os.File, int64, string, error) {
	action, err := r.Get(ctx, p, id, 0, 1)
	if err != nil {
		return nil, 0, "", err
	}
	if action.Kind != "file_download" || action.Status != "succeeded" {
		return nil, 0, "", errors.New("download is not complete")
	}
	rec, err := r.journal.Action(id)
	if err != nil {
		return nil, 0, "", err
	}
	expected, _ := r.artifactPath(id, ".data")
	if rec.Artifact != expected || rec.SHA256 == "" {
		return nil, 0, "", errors.New("download artifact is unavailable")
	}
	f, err := os.Open(expected)
	if err == nil {
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != rec.Size {
			_ = f.Close()
			return nil, 0, "", errors.New("download artifact length changed")
		}
	}
	return f, rec.Size, rec.SHA256, err
}

func (r *Runtime) filePayloadLimit(action Action) (int, error) {
	rec, err := r.journal.Action(action.ID)
	if err != nil {
		return 0, err
	}
	envelope := Event{EventID: strings.Repeat("0", 36), ActionID: action.ID, Sequence: ^uint64(0), State: "succeeded", Payload: map[string]any{}, OccurredAt: time.Unix(0, 1).UTC(), SessionToken: rec.SessionToken}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return 0, err
	}
	return maxEventBytes - len(encoded) + 2, nil // subtract empty {} payload
}

func textFileResult(file assetexec.FileResult, data []byte, limit int) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("file is not valid UTF-8; use download_file")
	}
	result := map[string]any{"file": file, "content": string(data)}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > limit {
		return nil, errors.New("encoded text exceeds the audit event limit; use download_file")
	}
	return result, nil
}

func listedFileResult(values []assetexec.FileInfo, truncated bool, limit int) map[string]any {
	result := map[string]any{"entries": values[:0], "truncated": truncated}
	for n := 1; n <= len(values); n++ {
		result["entries"] = values[:n]
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > limit {
			result["entries"] = values[:n-1]
			result["truncated"] = true
			break
		}
	}
	return result
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("file exceeds transfer limit")
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func (r *Runtime) executeFile(ctx context.Context, action Action, s *liveSession) (map[string]any, string) {
	var req fileRequest
	if err := decodeRequest(action.Request, &req); err != nil {
		return map[string]any{"error": err.Error()}, "failed"
	}
	s.mu.Lock()
	connection := s.connection
	s.mu.Unlock()
	payloadLimit, limitErr := r.filePayloadLimit(action)
	if limitErr != nil || payloadLimit < 1024 {
		return map[string]any{"error": "file audit metadata is unavailable or too large"}, "failed"
	}
	var result map[string]any
	var err error
	switch action.Kind {
	case "file_list":
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		if limit > 200 {
			limit = 200
		}
		values, truncated, listErr := connection.List(ctx, req.Path, limit)
		err = listErr
		result = listedFileResult(values, truncated, payloadLimit)
	case "file_stat":
		value, statErr := connection.Stat(ctx, req.Path)
		err = statErr
		result = map[string]any{"file": value}
	case "file_read":
		var data bytes.Buffer
		value, readErr := connection.ReadFile(ctx, req.Path, &boundedWriter{writer: &data, remaining: 64 << 10})
		if readErr != nil {
			return map[string]any{"error": readErr.Error()}, "failed"
		}
		result, err = textFileResult(value, data.Bytes(), payloadLimit)
	case "file_write":
		if len(req.Content) > 64<<10 {
			return map[string]any{"error": "use upload_file for files larger than 64 KiB"}, "failed"
		}
		hash := sha256.Sum256([]byte(req.Content))
		value, writeErr := connection.WriteFile(ctx, req.Path, strings.NewReader(req.Content), int64(len(req.Content)), hex.EncodeToString(hash[:]), req.Overwrite)
		err = writeErr
		result = map[string]any{"file": value}
	case "file_upload":
		rec, readErr := r.journal.Action(action.ID)
		if readErr != nil {
			return map[string]any{"error": "upload content is unavailable"}, "failed"
		}
		expected, _ := r.artifactPath(action.ID, ".data")
		if rec.Artifact != expected {
			return map[string]any{"error": "upload content is unavailable"}, "failed"
		}
		f, openErr := os.Open(expected)
		if openErr != nil {
			return map[string]any{"error": "upload content is unavailable"}, "failed"
		}
		defer f.Close()
		value, writeErr := connection.WriteFile(ctx, req.Path, f, req.Size, req.SHA256, req.Overwrite)
		err = writeErr
		result = map[string]any{"file": value}
	case "file_download":
		f, openErr := r.reserveFile(action.ID, r.maxTransferBytes())
		if openErr != nil {
			return map[string]any{"error": openErr.Error()}, "failed"
		}
		defer f.Close()
		defer func() {
			_ = os.Remove(f.Name())
		}()
		hash := sha256.New()
		writer := &boundedWriter{writer: io.MultiWriter(f, hash), remaining: r.maxTransferBytes()}
		value, readErr := connection.ReadFile(ctx, req.Path, writer)
		if readErr != nil {
			return map[string]any{"error": readErr.Error()}, "failed"
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		written := r.maxTransferBytes() - writer.remaining
		if value.Size != written || value.SHA256 != digest {
			return map[string]any{"error": "download length or checksum changed"}, "failed"
		}
		if err = ctx.Err(); err != nil {
			return map[string]any{"error": err.Error()}, "failed"
		}
		if err = r.persistArtifact(action.ID, f, written, digest); err != nil {
			return map[string]any{"error": "could not record download evidence"}, "failed"
		}
		result = map[string]any{"file": value, "size": value.Size, "sha256": digest}
	case "file_mkdir":
		err = connection.Mkdir(ctx, req.Path)
	case "file_rename":
		err = connection.Rename(ctx, req.Path, req.Destination)
	case "file_remove":
		err = connection.Remove(ctx, req.Path)
	default:
		return map[string]any{"error": "unsupported asset operation"}, "rejected"
	}
	if result == nil {
		result = map[string]any{"path": req.Path}
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if encoded, encodeErr := json.Marshal(result); encodeErr != nil || len(encoded) > payloadLimit {
		result = map[string]any{"error": "file result exceeds the audit event limit"}
		if err == nil {
			err = errors.New("file result exceeds the audit event limit")
		}
	}
	if err != nil {
		// Writes can have taken effect even if the transport lost its reply.
		if action.Kind == "file_write" || action.Kind == "file_upload" || action.Kind == "file_remove" || action.Kind == "file_rename" || action.Kind == "file_mkdir" {
			return result, "unknown"
		}
		return result, "failed"
	}
	return result, "succeeded"
}
