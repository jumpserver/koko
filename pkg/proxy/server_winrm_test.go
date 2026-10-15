package proxy

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestParserDrainsEchoWhileInputQueueIsFull(t *testing.T) {
	p := &Parser{protocolType: srvconn.ProtocolSSH, zmodemParser: zmodem.New()}
	if err := p.initial(80, 24); err != nil {
		t.Fatal(err)
	}
	processed := make(chan struct{}, 2)
	p.SetUserInputFilter(func(data []byte) []byte { processed <- struct{}{}; return data })
	input := make(chan *exchange.RoomMessage, 2)
	output := make(chan []byte, 1)
	_, echoed := p.ParseStream(input, output)
	t.Cleanup(func() {
		p.Close()
		for range echoed {
		}
	})
	for range 2 {
		input <- &exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("paste")}
		select {
		case <-processed:
		case <-time.After(2 * time.Second):
			t.Fatal("input was not processed")
		}
	}
	// Leave user output unread so one packet is buffered and the next is pending.
	output <- []byte("echo")
	select {
	case data := <-echoed:
		if string(data) != "echo" {
			t.Fatalf("unexpected output: %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("full input queue stopped echo processing")
	}
}

type sessionWriterTestConnection struct {
	srvconn.ServerConnection
	write  func([]byte) (int, error)
	resize func(int, int) error
}

func (c sessionWriterTestConnection) Write(data []byte) (int, error) { return c.write(data) }
func (c sessionWriterTestConnection) SetWinSize(w, h int) error      { return c.resize(w, h) }

type sessionWriterUserConnection struct {
	sessionWriterTestConnection
	input func([]byte, string) (int, error)
}

func (c sessionWriterUserConnection) WriteInput(data []byte, user string) (int, error) {
	return c.input(data, user)
}

func TestSessionWriterStreamsLargeInputAndResize(t *testing.T) {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	resized := make(chan srvconn.Windows, 1)
	requests, errors := startSessionWriter(sessionWriterTestConnection{
		write:  writer.Write,
		resize: func(w, h int) error { resized <- srvconn.Windows{Width: w, Height: h}; return nil },
	}, done, func(data []byte) []byte { return data })
	t.Cleanup(func() {
		close(done)
		_ = reader.Close()
		_ = writer.Close()
		for range errors {
		}
	})
	text := "#" + strings.Repeat("A", 100000)
	// Write blocks until echo is drained. The caller must remain free to read it.
	select {
	case requests <- sessionWriteRequest{data: []byte(text)}:
	case <-time.After(2 * time.Second):
		t.Fatal("input submission stalled")
	}
	echo := make(chan string, 1)
	go func() {
		data := make([]byte, len(text))
		_, _ = io.ReadFull(reader, data)
		echo <- string(data)
	}()
	select {
	case got := <-echo:
		if got != text {
			t.Fatal("large input was lost or truncated")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("echo stalled")
	}
	select {
	case requests <- sessionWriteRequest{window: &srvconn.Windows{Width: 120, Height: 40}}:
	case <-time.After(2 * time.Second):
		t.Fatal("resize stalled after large input")
	}
	select {
	case size := <-resized:
		if size.Width != 120 || size.Height != 40 {
			t.Fatalf("wrong size: %+v", size)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resize was not applied")
	}
}

func TestSessionWriterPreservesUserAndRejectsShortWrites(t *testing.T) {
	done := make(chan struct{})
	seen := make(chan string, 1)
	requests, errors := startSessionWriter(sessionWriterUserConnection{
		input: func(data []byte, user string) (int, error) {
			seen <- user + ":" + string(data)
			return len(data) - 1, nil
		},
	}, done, func(data []byte) []byte { return data })
	defer close(done)
	select {
	case requests <- sessionWriteRequest{data: []byte("Get-Location"), user: "shared user"}:
	case <-time.After(2 * time.Second):
		t.Fatal("input submission stalled")
	}
	select {
	case err := <-errors:
		if err != io.ErrShortWrite {
			t.Fatalf("short write was not reported: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write failure was not reported")
	}
	if got := <-seen; got != "shared user:Get-Location" {
		t.Fatalf("lost input attribution: %q", got)
	}
}

func TestSessionWriterHonorsPauseAndResumes(t *testing.T) {
	session := &SwitchSession{}
	session.pausedStatus.Store(true)
	done := make(chan struct{})
	written := make(chan string, 2)
	resized := make(chan struct{}, 1)
	requests, errors := startSessionWriter(sessionWriterTestConnection{
		write:  func(data []byte) (int, error) { written <- string(data); return len(data), nil },
		resize: func(int, int) error { resized <- struct{}{}; return nil },
	}, done, session.filterUserInput)
	t.Cleanup(func() {
		close(done)
		for range errors {
		}
	})
	for _, request := range []sessionWriteRequest{
		{data: []byte("paused input")},
		{window: &srvconn.Windows{Width: 80, Height: 24}},
	} {
		select {
		case requests <- request:
		case <-time.After(2 * time.Second):
			t.Fatal("paused input blocked the connection writer")
		}
	}
	select {
	case <-resized:
	case <-time.After(2 * time.Second):
		t.Fatal("resize was blocked by pause")
	}
	if len(written) != 0 {
		t.Fatal("paused input reached the connection")
	}
	session.pausedStatus.Store(false)
	select {
	case requests <- sessionWriteRequest{data: []byte("resumed input")}:
	case <-time.After(2 * time.Second):
		t.Fatal("input did not resume")
	}
	select {
	case got := <-written:
		if got != "resumed input" {
			t.Fatalf("unexpected resumed input: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resumed input was not written")
	}
}

func TestSessionWriterClosesBlockedWrite(t *testing.T) {
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	requests, errors := startSessionWriter(sessionWriterTestConnection{write: writer.Write}, ctx.Done(), func(data []byte) []byte { return data })
	t.Cleanup(func() {
		cancel()
		_ = reader.Close()
		_ = writer.Close()
		for range errors {
		}
	})
	select {
	case requests <- sessionWriteRequest{data: []byte("blocked")}:
	case <-time.After(2 * time.Second):
		t.Fatal("input submission stalled")
	}
	// Bridge shutdown cancels the writer and closes the connection to release I/O.
	cancel()
	_ = writer.Close()
	finished := make(chan struct{})
	go func() {
		for range errors {
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("connection writer leaked after shutdown")
	}
}

type winRMTestCommandStorage struct{ saved chan []*model.Command }

func (s winRMTestCommandStorage) TypeName() string { return "server" }
func (s winRMTestCommandStorage) BulkSave(commands []*model.Command) error {
	s.saved <- append([]*model.Command(nil), commands...)
	return nil
}

func TestCommandRecorderFlushesFinalQueue(t *testing.T) {
	saved := make(chan []*model.Command, 1)
	recorder := &CommandRecorder{queue: make(chan *model.Command, 2), closed: make(chan struct{}),
		storage: winRMTestCommandStorage{saved}}
	recorder.Record(&model.Command{Input: "Get-Location"})
	recorder.Record(&model.Command{Input: "Get-Service"})
	recorder.End()
	done := make(chan struct{})
	go func() { recorder.record(); close(done) }()
	select {
	case commands := <-saved:
		if len(commands) != 2 || commands[1].Input != "Get-Service" {
			t.Fatalf("final commands lost: %+v", commands)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("final commands were not saved")
	}
	<-done
}

func TestWinRMRejectedCommandIsAudited(t *testing.T) {
	recorder := &CommandRecorder{queue: make(chan *model.Command, 1)}
	server := &Server{ID: "session", account: &model.Account{BaseAccount: model.BaseAccount{Username: "operator"}}, backgroundRecorder: recorder,
		connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{Protocol: srvconn.ProtocolWinRM,
			Asset: model.Asset{Name: "windows", OrgID: "org"}, ExpireAt: model.ExpireInfo(time.Now().Add(time.Hour).Unix())}},
		commandACLs: model.CommandACLs{{ID: "acl", Action: model.ActionReject, CommandGroups: []model.CommandFilterItem{{ID: "group", RePattern: `(?i)Restart-Service`}}}}}
	command := "Restart-Service Spooler; Write-Output '中文'"
	// A nil connection proves a rejected command never reaches the target.
	if err := server.executeWinRMCommand(nil, context.Background(), command, "shared user", io.Discard); err == nil {
		t.Fatal("ACL rejection was bypassed")
	}
	record := <-recorder.queue
	if record.Input != command || record.User != "shared user" || record.SessionID != "session" || record.OrgID != "org" ||
		record.RiskLevel != model.RejectLevel || record.CmdFilterAclId != "acl" || record.CmdGroupId != "group" || !strings.Contains(record.Output, "rejected") {
		t.Fatalf("incomplete command audit: %+v", record)
	}
}

func TestCommandReviewAuditLevels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rule     model.CommandAction
		decision model.CommandAction
		reviewed bool
		risk     int64
	}{
		{"reject", model.ActionReject, model.ActionReject, false, model.RejectLevel},
		{"review reject", model.ActionReview, model.ActionReject, false, model.ReviewReject},
		{"review cancel", model.ActionReview, model.ActionReview, false, model.ReviewCancel},
		{"review accept", model.ActionReview, model.ActionAccept, true, model.ReviewAccept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &CommandRecorder{queue: make(chan *model.Command, 1)}
			server := &Server{sessionInfo: &model.Session{ID: "session"}, backgroundRecorder: recorder,
				commandACLs: model.CommandACLs{{ID: "acl", Action: tc.rule}}}
			server.RecordBackgroundCommand("Get-Location", "audit output", nil, &CommandACLDecision{
				Action: tc.decision, ACLID: "acl", ItemID: "group", Reviewed: tc.reviewed,
			})
			record := <-recorder.queue
			if record.RiskLevel != tc.risk || record.CmdFilterAclId != "acl" || record.CmdGroupId != "group" {
				t.Fatalf("incorrect ACL audit: %+v", record)
			}
		})
	}
}
