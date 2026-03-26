package auth

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	gossh "golang.org/x/crypto/ssh"
)

func TestBuildDirectSFTPChallengeBridgeAutoFillsPasswordAndForwardsOTP(t *testing.T) {
	token := &model.ConnectToken{
		Account: model.Account{
			BaseAccount: model.BaseAccount{
				Username: "halo123",
				Secret:   "diaosi1234dsa",
			},
		},
	}

	var (
		gotUser        string
		gotInstruction string
		gotQuestions   []string
		gotEchos       []bool
	)
	bridge := buildDirectSFTPChallengeBridge("session-1", token, func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		gotUser = user
		gotInstruction = instruction
		gotQuestions = append([]string(nil), questions...)
		gotEchos = append([]bool(nil), echos...)
		return []string{"831248"}, nil
	})

	answers, err := bridge("", "otp required", []string{"Password:", "Verification code:"}, []bool{false, false})
	if err != nil {
		t.Fatalf("bridge returned error: %v", err)
	}

	if want := []string{"diaosi1234dsa", "831248"}; !reflect.DeepEqual(answers, want) {
		t.Fatalf("answers mismatch: got %v want %v", answers, want)
	}
	if gotUser != "halo123" {
		t.Fatalf("challenge user mismatch: got %q want %q", gotUser, "halo123")
	}
	if gotInstruction != "otp required" {
		t.Fatalf("instruction mismatch: got %q want %q", gotInstruction, "otp required")
	}
	if want := []string{"Verification code:"}; !reflect.DeepEqual(gotQuestions, want) {
		t.Fatalf("forwarded questions mismatch: got %v want %v", gotQuestions, want)
	}
	if want := []bool{false}; !reflect.DeepEqual(gotEchos, want) {
		t.Fatalf("forwarded echos mismatch: got %v want %v", gotEchos, want)
	}
}

func TestDirectSFTPAssetAuthStateSubmitPromptAnswersClearsPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := newDirectSFTPAssetAuthState("session-1", &model.ConnectToken{}, ctx.Done())
	resultCh := make(chan directSFTPAssetPromptAnswer, 1)
	go func() {
		answers, err := state.awaitClientAnswers("halo123", "otp required", []string{"Verification code:"}, []bool{false})
		resultCh <- directSFTPAssetPromptAnswer{answers: answers, err: err}
	}()

	event := state.waitForNextEvent()
	if event.prompt == nil {
		t.Fatal("expected prompt event")
	}
	if got := state.currentPendingPrompt(); got != event.prompt {
		t.Fatal("expected current prompt to match prompt event")
	}

	if err := state.submitPromptAnswers([]string{"797131"}, nil); err != nil {
		t.Fatalf("submit answers failed: %v", err)
	}
	if got := state.currentPendingPrompt(); got != nil {
		t.Fatal("expected prompt to be cleared immediately after submit")
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatalf("await answers returned error: %v", result.err)
	}
	if want := []string{"797131"}; !reflect.DeepEqual(result.answers, want) {
		t.Fatalf("answer mismatch: got %v want %v", result.answers, want)
	}
}

func TestDirectSFTPAssetAuthStateUsesSingleOutboundHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()

	var handshakeCount int32
	server := &ssh.Server{
		ConnCallback: func(ctx ssh.Context, conn net.Conn) net.Conn {
			atomic.AddInt32(&handshakeCount, 1)
			return conn
		},
		KeyboardInteractiveHandler: func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) error {
			answers, err := challenger(ctx.User(), "otp required", []string{"Password:", "Verification code:"}, []bool{false, false})
			if err != nil {
				return err
			}
			if len(answers) != 2 {
				return errors.New("answers mismatch")
			}
			if answers[0] != "diaosi1234dsa" {
				return errors.New("password mismatch")
			}
			if answers[1] != "831248" {
				return errors.New("otp mismatch")
			}
			return nil
		},
	}
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
		select {
		case serveErr := <-serverErrCh:
			if serveErr != nil && !errors.Is(serveErr, ssh.ErrServerClosed) {
				t.Fatalf("server exited with error: %v", serveErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for test ssh server to exit")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := listener.Addr().(*net.TCPAddr)
	state := newDirectSFTPAssetAuthState("session-1", &model.ConnectToken{
		Protocol: model.ProtocolSFTP,
		Account: model.Account{
			BaseAccount: model.BaseAccount{
				Username: "halo123",
				Secret:   "diaosi1234dsa",
			},
		},
		Asset: model.Asset{
			Address: "127.0.0.1",
			Protocols: []model.Protocol{
				{Name: model.ProtocolSFTP, Port: addr.Port, Public: true},
			},
		},
	}, ctx.Done())

	state.startHandshake()
	event := state.waitForNextEvent()
	if event.err != nil {
		t.Fatalf("unexpected prompt wait error: %v", event.err)
	}
	if event.prompt == nil {
		t.Fatal("expected interactive prompt event")
	}
	if want := []string{"Verification code:"}; !reflect.DeepEqual(event.prompt.Questions, want) {
		t.Fatalf("prompt questions mismatch: got %v want %v", event.prompt.Questions, want)
	}

	if err := state.submitPromptAnswers([]string{"831248"}, nil); err != nil {
		t.Fatalf("submit prompt answers failed: %v", err)
	}

	event = state.waitForNextEvent()
	if event.err != nil {
		t.Fatalf("unexpected prepared wait error: %v", event.err)
	}
	if event.prepared == nil || event.prepared.Client == nil {
		t.Fatal("expected prepared direct sftp client")
	}
	_ = event.prepared.Client.Close()

	if got := atomic.LoadInt32(&handshakeCount); got != 1 {
		t.Fatalf("handshake count mismatch: got %d want %d", got, 1)
	}
}
