package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
)

type ExportView struct {
	ID          string           `json:"id"`
	ExecutionID string           `json:"execution_id,omitempty"`
	ConsoleID   string           `json:"console_id,omitempty"`
	Format      string           `json:"format"`
	Filename    string           `json:"filename"`
	Mode        string           `json:"mode"`
	Status      string           `json:"status"`
	Rows        int64            `json:"rows"`
	Bytes       int64            `json:"bytes"`
	Ready       bool             `json:"ready"`
	Truncated   bool             `json:"truncated"`
	Error       *APIError        `json:"error,omitempty"`
	ExpiresAt   *time.Time       `json:"expires_at,omitempty"`
	Limits      map[string]int64 `json:"limits"`
}
type exportJob struct {
	cachedRows                                                      *int // immutable requested prefix, including zero; nil exports the entire cache
	expiryTimer                                                     *time.Timer
	stopSessionWatch                                                func() bool
	mu                                                              sync.Mutex
	id, format, mode, resultID, path, status, filename, namePrefix  string
	createdAt                                                       time.Time
	session                                                         *session
	server                                                          *Server
	execution                                                       *execution
	ctx                                                             context.Context
	cancel                                                          context.CancelFunc
	rows, bytes                                                     int64
	limitRows, limitBytes                                           int64
	prepared, approved, active, deleteRequested, expired, truncated bool
	err                                                             *APIError
	expiresAt                                                       *time.Time
	lastProgress                                                    time.Time
}
type downloadTicket struct {
	job     *exportJob
	expires time.Time
}

func (v *Server) submitExport(w http.ResponseWriter, r *http.Request) {
	v.submitExecution(w, r, executionIntent{export: true}, nil)
}

// Workers wait here so accepted exports stay queued without starting a query
// or creating a partial file while the export workers are busy.
func (v *Server) acquireExportSlot(ctx context.Context) bool {
	select {
	case v.exportSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (v *Server) newExportLocked(s *session, format, mode string, e *execution) (*exportJob, error) {
	if v.exportInitErr != nil {
		return nil, &APIError{"export_storage_unavailable", "Private export storage could not be initialized"}
	}
	if format != "csv" && format != "xlsx" {
		return nil, &APIError{"invalid_export_format", "Choose CSV or XLSX"}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	j := &exportJob{id: newID("exp_"), format: format, mode: mode, session: s, server: v, execution: e, ctx: ctx, cancel: cancel, status: "queued"}
	j.createdAt = time.Now()
	j.filename = exportFilename("query", format, j.createdAt)
	if e != nil {
		if e.browse != nil && e.browse.plan != nil {
			j.namePrefix = exportSourcePrefix(s.grant.Dialect(), e.browse, nil, nil)
		} else if e.queryPage != nil {
			if result := s.results[e.queryPage.sourceResultID]; result != nil {
				j.namePrefix = resultExportPrefix(s.grant.Dialect(), result)
			}
		}
		if j.namePrefix != "" {
			j.filename = exportFilename(j.namePrefix, format, j.createdAt)
		}
	}
	if format == "xlsx" {
		j.limitRows = (1 << 20) - 1 // One worksheet row is used for column headers.
	}
	if s.exports == nil {
		s.exports = map[string]*exportJob{}
	}
	s.exports[j.id] = j
	j.mu.Lock()
	j.stopSessionWatch = context.AfterFunc(s.ctx, func() { j.cleanup(false) })
	j.mu.Unlock()
	return j, nil
}
func (j *exportJob) snapshot() ExportView {
	j.mu.Lock()
	view := ExportView{ID: j.id, Format: j.format, Mode: j.mode, Status: j.status, Rows: j.rows, Bytes: j.bytes, Ready: j.prepared && j.approved && !j.expired && !j.deleteRequested && j.path != "" && j.ctx.Err() == nil, Truncated: j.truncated, Error: j.err, ExpiresAt: j.expiresAt, Limits: map[string]int64{"max_rows": j.limitRows, "max_bytes": j.limitBytes, "max_cell_chars": exportMaxCellChars, "max_duration_seconds": int64(j.server.opts.QueryTimeout / time.Second)}}
	view.Filename = j.filename
	if j.format != "xlsx" {
		view.Limits["max_cell_chars"] = 0
	}
	expired := j.expired || (j.expiresAt != nil && time.Now().After(*j.expiresAt))
	j.mu.Unlock()
	if e := j.execution; e != nil {
		ev := e.snapshot()
		view.ExecutionID = ev.ID
		view.ConsoleID = ev.ConsoleID
		view.Status = ev.Status
		if view.Error == nil || ev.CancelReason == "timeout" && ev.Error != nil {
			view.Error = ev.Error
		}
	}
	if expired {
		view.Status = "expired"
		view.Ready = false
	}
	return view
}
func (j *exportJob) emit() { j.session.emit("export.updated", j.snapshot()) }
func (j *exportJob) cleanup(expired bool) {
	j.cancel()
	j.mu.Lock()
	j.deleteRequested = true
	j.expired = j.expired || expired
	if j.active {
		j.mu.Unlock()
		return
	}
	path := j.path
	j.path = ""
	j.approved = false
	j.mu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
	j.mu.Lock()
	if j.expiryTimer != nil {
		j.expiryTimer.Stop()
	}
	stop := j.stopSessionWatch
	j.mu.Unlock()
	if stop != nil {
		stop()
	}
}
func (j *exportJob) finishExecution() {
	e := j.execution.snapshot()
	j.mu.Lock()
	j.approved = e.Status == "succeeded" && e.AuditStatus == "recorded" && j.prepared && j.err == nil && !j.deleteRequested && j.ctx.Err() == nil && j.path != "" && j.session.grant.Check() == nil
	j.mu.Unlock()
	if !j.snapshot().Ready {
		j.cleanup(false)
	}
	j.emit()
}
func (j *exportJob) fail(err error) {
	api := &APIError{"export_failed", "Export could not be completed"}
	var explicit *APIError
	if errors.As(err, &explicit) {
		copy := *explicit
		api = &copy
	} else if errors.Is(err, context.Canceled) {
		api = &APIError{"export_canceled", "Export canceled"}
	} else if errors.Is(err, context.DeadlineExceeded) {
		api = &APIError{"export_timeout", "Export time limit exceeded"}
	}
	j.mu.Lock()
	j.err = api
	j.status = "failed"
	if errors.Is(err, context.Canceled) {
		j.status = "canceled"
	}
	j.mu.Unlock()
}
func (j *exportJob) progress(row []any) error {
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	j.mu.Lock()
	if j.limitRows > 0 && j.rows >= j.limitRows {
		j.mu.Unlock()
		return &APIError{"export_row_limit", "Export row limit reached; no partial file will be offered"}
	}
	if j.limitBytes > 0 && j.bytes+int64(len(data)) > j.limitBytes {
		j.mu.Unlock()
		return &APIError{"export_byte_limit", "Export exceeds the configured data limit"}
	}
	j.rows++
	j.bytes += int64(len(data))
	notify := time.Since(j.lastProgress) >= 250*time.Millisecond
	if notify {
		j.lastProgress = time.Now()
	}
	j.mu.Unlock()
	if notify {
		j.emit()
	}
	return nil
}
func (j *exportJob) allowed(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := j.ctx.Err(); err != nil {
		return err
	}
	if !j.session.grant.Permissions.Download {
		return &APIError{"download_permission_denied", "Result download is not permitted"}
	}
	if err := j.session.grant.Check(); err != nil {
		return &APIError{"session_unavailable", "Session is unavailable"}
	}
	return nil
}

type exportFileWriter struct {
	file   *os.File
	n, max int64
}

func (w *exportFileWriter) Write(b []byte) (int, error) {
	if w.max > 0 && w.n+int64(len(b)) > w.max {
		return 0, &APIError{"export_byte_limit", "Export file exceeds the configured disk limit"}
	}
	n, err := w.file.Write(b)
	w.n += int64(n)
	return n, err
}
func (j *exportJob) produce(ctx context.Context, columns []Column, next func() ([]any, bool, error)) (err error) {
	if err = j.allowed(ctx); err != nil {
		return err
	}
	j.mu.Lock()
	j.active = true
	j.status = "running"
	j.mu.Unlock()
	j.emit()
	defer func() {
		j.mu.Lock()
		j.active = false
		remove := j.deleteRequested || err != nil
		j.mu.Unlock()
		if err != nil {
			j.fail(err)
		}
		if remove {
			j.cleanup(false)
		}
		j.emit()
	}()
	dir := j.server.exportDir
	if dir == "" {
		return &APIError{"export_storage_unavailable", "Private export storage is unavailable"}
	}
	file, err := os.CreateTemp(dir, ".export-")
	if err != nil {
		return err
	}
	defer file.Close()
	j.mu.Lock()
	j.path = file.Name()
	j.mu.Unlock()
	sink := &exportFileWriter{file: file, max: j.limitBytes}
	var writer exportRowWriter
	if j.format == "xlsx" {
		writer, err = newXLSXExportWriter(sink, columns)
	} else {
		writer, err = newCSVExportWriter(sink, columns)
	}
	if err != nil {
		return err
	}
	for {
		if err = j.allowed(ctx); err != nil {
			return err
		}
		var row []any
		var ok bool
		row, ok, err = next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if err = j.progress(row); err != nil {
			return err
		}
		if err = writer.Row(row); err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = j.allowed(ctx); err != nil {
		return err
	}
	expiry := time.Now().Add(15 * time.Minute)
	j.mu.Lock()
	j.prepared = true
	j.expiresAt = &expiry
	j.mu.Unlock()
	j.mu.Lock()
	j.expiryTimer = time.AfterFunc(15*time.Minute, func() { j.cleanup(true); j.emit() })
	j.mu.Unlock()
	return nil
}
func (v *Server) collectExport(s *session, e *execution, rows *sql.Rows) (err error) {
	j := e.export
	prefix := j.namePrefix
	if prefix == "" {
		prefix = executionExportPrefix(s, e)
	}
	j.mu.Lock()
	j.filename = exportFilename(prefix, j.format, j.createdAt)
	j.mu.Unlock()
	defer func() {
		if err != nil {
			e.mu.RLock()
			interrupt := e.interruptBackend
			e.mu.RUnlock()
			if interrupt != nil {
				interrupt()
			}
		}
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if err != nil {
			j.fail(err)
			j.cleanup(false)
		}
	}()
	types, err := rows.ColumnTypes()
	if err != nil {
		return err
	}
	columns := make([]Column, len(types))
	labels := make([]string, len(types))
	for i, t := range types {
		columns[i] = Column{t.Name(), t.DatabaseTypeName()}
		labels[i] = t.Name()
	}
	sources := e.resultMaskColumns(s.grant.Dialect(), labels)
	err = j.produce(e.ctx, columns, func() ([]any, bool, error) {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return nil, false, err
			}
			if rows.NextResultSet() {
				return nil, false, &APIError{"export_result_limit", "Only one result set can be exported"}
			}
			if err := rows.Err(); err != nil {
				return nil, false, err
			}
			if err := rows.Close(); err != nil {
				return nil, false, err
			}
			e.finishDatabaseWork()
			return nil, false, e.restoreSourceReadContext()
		}
		raw := make([]any, len(types))
		ptrs := make([]any, len(raw))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, false, err
		}
		for i := range raw {
			value, err := materializeValue(raw[i], types[i].DatabaseTypeName(), int(^uint(0)>>1), s.grant.Dialect())
			if err != nil {
				return nil, false, err
			}
			raw[i], _ = v.provider.Mask(s.grant, sources[i], types[i].Name(), value)
		}
		return raw, true, nil
	})
	return err
}
func (v *Server) createCachedExport(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	if !s.grant.Permissions.Download {
		fail(w, 403, "download_permission_denied", "Result download is not permitted")
		return
	}
	var request struct {
		Format    string `json:"format"`
		RequestID string `json:"request_id"`
		Rows      *int   `json:"rows,omitempty"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.RequestID == "" || len(request.RequestID) > 128 {
		fail(w, 400, "request_id_required", "A stable request_id is required")
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	if s.exportRequests == nil {
		s.exportRequests = map[string]*exportJob{}
	}
	if old := s.exportRequests[request.RequestID]; old != nil {
		s.mu.Unlock()
		if old.format != request.Format || old.resultID != r.PathValue("rid") || (old.cachedRows == nil) != (request.Rows == nil) || (old.cachedRows != nil && request.Rows != nil && *old.cachedRows != *request.Rows) {
			fail(w, 409, "idempotency_conflict", "Request id belongs to another export")
			return
		}
		writeJSON(w, 202, old.snapshot())
		return
	}
	result := s.results[r.PathValue("rid")]
	if result == nil {
		s.mu.Unlock()
		fail(w, 404, "not_found", "Result not found")
		return
	}
	count := len(result.Rows)
	if request.Rows != nil {
		if *request.Rows < 0 || *request.Rows > count {
			s.mu.Unlock()
			fail(w, 400, "invalid_export_rows", "Requested cached rows are unavailable")
			return
		}
		count = *request.Rows
	}
	if s.cachedExportWorkers >= 256 {
		s.mu.Unlock()
		fail(w, 429, "queue_full", "Session export queue is full")
		return
	}
	j, err := v.newExportLocked(s, request.Format, "cached", nil)
	if err != nil {
		s.mu.Unlock()
		exportAPIError(w, err)
		return
	}
	j.resultID = result.ID
	j.filename = exportFilename(resultExportPrefix(s.grant.Dialect(), result), j.format, j.createdAt)
	j.cachedRows = request.Rows
	j.truncated = result.Truncated
	s.exportRequests[request.RequestID] = j
	s.cachedExportWorkers++
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer func() {
			s.mu.Lock()
			s.cachedExportWorkers--
			s.mu.Unlock()
		}()
		if v.acquireExportSlot(j.ctx) {
			defer func() { <-v.exportSlots }()
		}
		ctx := j.ctx
		if v.opts.QueryTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, v.opts.QueryTimeout)
			defer cancel()
		}
		i := 0
		err := j.produce(ctx, result.Columns, func() ([]any, bool, error) {
			if i >= count {
				return nil, false, nil
			}
			row := result.Rows[i]
			i++
			return row, true, nil
		})
		now := time.Now()
		auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		auditErr := v.provider.Record(auditCtx, s.grant, core.AuditRecord{ID: j.id, RiskClassified: true, SQL: "/* Chen cached result export */", Output: fmt.Sprintf("export_id=%s result_id=%s format=%s rows=%d cached_truncated=%v complete=%v", j.id, j.resultID, j.format, j.snapshot().Rows, j.truncated, err == nil), StartedAt: now, FinishedAt: now})
		auditCancel()
		if err == nil && auditErr != nil {
			err = &APIError{"audit_persistence_failed", "Export could not be audited"}
		}
		if err != nil {
			j.fail(err)
			j.cleanup(false)
		} else {
			j.mu.Lock()
			j.status = "succeeded"
			j.approved = !j.deleteRequested && j.ctx.Err() == nil && j.path != "" && s.grant.Check() == nil
			approved := j.approved
			j.mu.Unlock()
			if !approved {
				reason := j.allowed(ctx)
				if reason == nil {
					reason = &APIError{"export_unavailable", "Export is no longer available"}
				}
				j.fail(reason)
				// Join cleanup here as well as the session cancellation callback:
				// an audit can finish before that callback has been scheduled.
				j.cleanup(false)
			}
		}
		j.emit()
	}()
	writeJSON(w, 202, j.snapshot())
}
func exportAPIError(w http.ResponseWriter, err error) {
	var api *APIError
	if errors.As(err, &api) {
		status := 400
		if api.Code == "export_storage_unavailable" {
			status = 503
		}
		fail(w, status, api.Code, api.Message)
	} else {
		fail(w, 500, "export_failed", "Unable to create export")
	}
}
func (v *Server) findExport(w http.ResponseWriter, r *http.Request) (*session, *exportJob) {
	s := v.auth(w, r, true)
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	j := s.exports[r.PathValue("xid")]
	s.mu.Unlock()
	if j == nil {
		fail(w, 404, "not_found", "Export not found")
		return nil, nil
	}
	return s, j
}
func (v *Server) exportStatus(w http.ResponseWriter, r *http.Request) {
	_, j := v.findExport(w, r)
	if j != nil {
		writeJSON(w, 200, j.snapshot())
	}
}
func (v *Server) cancelExport(w http.ResponseWriter, r *http.Request) {
	_, j := v.findExport(w, r)
	if j == nil {
		return
	}
	if j.execution != nil {
		r.SetPathValue("eid", j.execution.view.ID)
		v.cancelExecutionWithResponse(w, r, func(ExecutionView) any { return j.snapshot() })
		return
	}
	j.mu.Lock()
	if terminal(j.status) || j.expired {
		j.mu.Unlock()
		writeJSON(w, 200, j.snapshot())
		return
	}
	// Serialize cancellation with the cached worker's final approval.
	j.cancel()
	j.status = "cancel_requested"
	j.mu.Unlock()
	writeJSON(w, 202, j.snapshot())
}
func (v *Server) deleteExport(w http.ResponseWriter, r *http.Request) {
	_, j := v.findExport(w, r)
	if j == nil {
		return
	}
	if !terminal(j.snapshot().Status) && j.snapshot().Status != "expired" {
		fail(w, 409, "export_active", "Cancel the active export first")
		return
	}
	j.cleanup(true)
	j.emit()
	w.WriteHeader(204)
}
func (v *Server) exportDownloadTicket(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	if !s.grant.Permissions.Download {
		fail(w, 403, "download_permission_denied", "Result download is not permitted")
		return
	}
	s.mu.Lock()
	j := s.exports[r.PathValue("xid")]
	s.mu.Unlock()
	if j == nil {
		fail(w, 404, "not_found", "Export not found")
		return
	}
	if !j.snapshot().Ready {
		fail(w, 409, "export_not_ready", "Export is not ready or has expired")
		return
	}
	token := newID("dl_")
	expires := time.Now().Add(time.Minute)
	v.mu.Lock()
	for key, ticket := range v.downloads {
		if time.Now().After(ticket.expires) {
			delete(v.downloads, key)
		}
	}
	if len(v.downloads) >= 64 {
		v.mu.Unlock()
		fail(w, 429, "download_ticket_limit", "Too many active download tickets")
		return
	}
	v.downloads[tokenKey(token)] = downloadTicket{j, expires}
	v.mu.Unlock()
	writeJSON(w, 201, map[string]any{"download_url": apiPath + "/downloads", "method": "POST", "fields": map[string]string{"ticket": token}, "expires_at": expires})
}
func (v *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		fail(w, 415, "form_required", "Native form download required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil || len(r.PostForm) != 1 || len(r.PostForm["ticket"]) != 1 {
		fail(w, 400, "invalid_ticket", "Invalid download ticket")
		return
	}
	key := tokenKey(r.PostForm.Get("ticket"))
	v.mu.Lock()
	ticket, ok := v.downloads[key]
	delete(v.downloads, key)
	v.mu.Unlock()
	if !ok || time.Now().After(ticket.expires) {
		fail(w, 410, "download_ticket_expired", "Download ticket was used or expired")
		return
	}
	j := ticket.job
	if err := j.allowed(r.Context()); err != nil || !j.snapshot().Ready {
		fail(w, 403, "download_unavailable", "Export permission or state changed")
		return
	}
	j.mu.Lock()
	path := j.path
	filename := j.filename
	j.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		fail(w, 410, "export_expired", "Export file is unavailable")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		fail(w, 500, "export_unavailable", "Export file is unavailable")
		return
	}
	contentType := "text/csv; charset=utf-8"
	if j.format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Content-Length", fmt.Sprint(stat.Size()))
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Chen-Result-Truncated", fmt.Sprint(j.truncated))
	buffer := make([]byte, 64<<10)
	for {
		if j.allowed(r.Context()) != nil {
			return
		}
		n, err := f.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
	}
}
