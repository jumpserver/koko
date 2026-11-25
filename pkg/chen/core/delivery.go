package core

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/proxy"
)

// The configuration is loaded once per flush so newly selected storage takes
// effect and several commands can be delivered with a single Core config read.
func (p *CoreProvider) deliver(ctx context.Context, e *envelope, config func() (model.TerminalConfig, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var pending error
	if e.Replay != nil && !e.Replay.Confirmed {
		pending = p.deliverReplay(ctx, e.Replay, config)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.TicketCancel != nil {
		req := *e.TicketCancel
		if !validTicketRequest(req, false) {
			return errors.New("invalid_ticket_cancel_request")
		}
		if _, err := p.client.Call(req.Method, req.URL, nil, nil); err != nil {
			pending = errors.Join(pending, err)
		} else {
			e.TicketCancel = nil
		}
	}
	if e.Command != nil {
		commands := []*model.Command{e.Command}
		if !e.CommandSaved {
			conf, err := config()
			if err != nil {
				return err
			}
			storage := proxy.NewCommandStorage(p.client, &conf)
			err = storage.BulkSave(commands)
			if err != nil && storage.TypeName() != "server" {
				err = p.client.PushSessionCommand(commands)
			}
			if err != nil {
				return err
			}
			e.CommandSaved = true
		}
		if risk := e.Command.RiskLevel; risk >= model.WarningLevel && risk <= model.ReviewReject {
			if err := p.client.NotifyCommand(commands); err != nil {
				return err
			}
		}
		e.Command = nil
	}
	if e.Lifecycle != nil {
		event := e.Lifecycle
		if err := p.client.RecordSessionLifecycleLog(event.SessionID, event.Event, model.SessionLifecycleLog{Reason: event.Reason}); err != nil {
			pending = errors.Join(pending, err)
		} else {
			e.Lifecycle = nil
		}
	}
	if e.Finish != nil && (e.Replay == nil || e.Replay.Confirmed) {
		f := e.Finish
		data := map[string]any{"is_finished": true, "date_end": common.ParseUnixTime(f.DateEnd), "is_success": f.Connected}
		resp, err := p.client.Call(http.MethodPatch, fmt.Sprintf(service.SessionDetailURL, f.SessionID), data, nil)
		// A compensating finish is also queued if session creation had an
		// unknown outcome. A missing session has no remaining Core resource.
		if err != nil && (resp == nil || resp.StatusCode != http.StatusNotFound) {
			pending = errors.Join(pending, err)
		} else {
			e.Finish = nil
		}
	}
	return pending
}

type replayTransfer struct {
	id      string
	done    chan struct{}
	request replayRequest
	err     error
}

// SDK and object-storage uploads have no request context. A single transfer
// stays owned until its final result is collected, even across flush timeouts.
// A slow upload therefore neither blocks command delivery nor starts copies.
func (p *CoreProvider) deliverReplay(ctx context.Context, replay *replayRequest, config func() (model.TerminalConfig, error)) error {
	transfer := p.replays.transfer
	if transfer == nil {
		if !p.startRequest() {
			return ErrClosed
		}
		transfer = &replayTransfer{id: replay.SessionID, done: make(chan struct{}), request: *replay}
		p.replays.transfer = transfer
		go func() {
			defer p.requests.Done()
			defer close(transfer.done)
			transfer.err = p.uploadReplay(&transfer.request, config)
		}()
	}
	if transfer.id != replay.SessionID {
		return errors.New("another_replay_upload_in_progress")
	}
	timeout, cancel := context.WithTimeout(ctx, p.options.RequestTimeout)
	defer cancel()
	select {
	case <-transfer.done:
		*replay = transfer.request
		p.replays.transfer = nil
		return transfer.err
	case <-timeout.Done():
		return timeout.Err()
	}
}

func (p *CoreProvider) uploadReplay(replay *replayRequest, config func() (model.TerminalConfig, error)) error {
	if !replay.Uploaded {
		conf, err := config()
		if err != nil {
			return err
		}
		storage := proxy.NewReplayStorage(p.client, &conf)
		if storage.TypeName() == "null" {
			// With recording disabled Core must not advertise a downloadable file.
			err = p.client.RecordSessionLifecycleLog(replay.SessionID, model.ReplayUploadFailure,
				model.SessionLifecycleLog{Reason: string(model.ReasonErrNullStorage)})
			replay.Confirmed = err == nil
			return err
		}
		archive, err := p.replays.finish(replay.SessionID)
		if err != nil {
			return err
		}
		if archive == nil {
			return os.ErrNotExist
		}
		name := replay.SessionID + ".cast.gz"
		file := filepath.Join(p.replays.dir, name)
		stat, err := os.Stat(file)
		if err != nil {
			return err
		}
		target, err := replayTarget(file)
		if err != nil {
			return err
		}
		err = storage.Upload(file, target)
		if err != nil && storage.TypeName() != "server" {
			err = p.client.UploadReplay(replay.SessionID, file, model.Version3)
		}
		if err != nil {
			return err
		}
		replay.Uploaded, replay.Size = true, stat.Size()
	}
	if _, err := p.client.FinishReplyWithSize(replay.SessionID, replay.Size); err != nil {
		return err
	}
	replay.Confirmed = true
	return nil
}

// A replay may finish or be recovered on a different UTC date than the SQL
// session started. Core looks up external storage using the session start date.
func replayTarget(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	archive, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	var header struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.NewDecoder(io.LimitReader(archive, 8<<10)).Decode(&header); err != nil {
		return "", err
	}
	if header.Timestamp <= 0 {
		return "", errors.New("invalid_replay_timestamp")
	}
	day := time.Unix(header.Timestamp, 0).UTC().Format(time.DateOnly)
	return day + "/" + filepath.Base(file), nil
}
