package sshx11

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

type sshPair struct {
	server        *gossh.ServerConn
	serverChans   <-chan gossh.NewChannel
	serverReqs    <-chan *gossh.Request
	client        *gossh.Client
	serverNetConn net.Conn
	clientNetConn net.Conn
}

func newSSHPair(t *testing.T) *sshPair {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey() error = %v", err)
	}
	serverConfig := &gossh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	type serverResult struct {
		conn    *gossh.ServerConn
		chans   <-chan gossh.NewChannel
		reqs    <-chan *gossh.Request
		netConn net.Conn
		err     error
	}
	serverResultCh := make(chan serverResult, 1)
	go func() {
		serverNetConn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResultCh <- serverResult{err: acceptErr}
			return
		}
		conn, chans, reqs, handshakeErr := gossh.NewServerConn(serverNetConn, serverConfig)
		serverResultCh <- serverResult{
			conn:    conn,
			chans:   chans,
			reqs:    reqs,
			netConn: serverNetConn,
			err:     handshakeErr,
		}
	}()

	clientConfig := &gossh.ClientConfig{
		User:            "test",
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	clientNetConn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("DialTimeout() error = %v", err)
	}
	clientConn, clientChans, clientReqs, err := gossh.NewClientConn(
		clientNetConn, listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("NewClientConn() error = %v", err)
	}
	result := <-serverResultCh
	if result.err != nil {
		t.Fatalf("NewServerConn() error = %v", result.err)
	}

	return &sshPair{
		server:        result.conn,
		serverChans:   result.chans,
		serverReqs:    result.reqs,
		client:        gossh.NewClient(clientConn, clientChans, clientReqs),
		serverNetConn: result.netConn,
		clientNetConn: clientNetConn,
	}
}

func (p *sshPair) close() {
	_ = p.client.Close()
	_ = p.server.Close()
	_ = p.clientNetConn.Close()
	_ = p.serverNetConn.Close()
}

func TestForwardBridgesTargetX11ChannelToOriginClient(t *testing.T) {
	origin := newSSHPair(t)
	defer origin.close()
	go gossh.DiscardRequests(origin.serverReqs)
	originX11Channels := origin.client.HandleChannelOpen(ChannelType)

	target := newSSHPair(t)
	defer target.close()
	go gossh.DiscardRequests(target.serverReqs)

	targetSession, targetSessionChannel, targetSessionRequests := openSession(t, target)
	defer targetSession.Close()
	defer targetSessionChannel.Close()

	type originator struct {
		Address string
		Port    uint32
	}
	x11ExtraData := gossh.Marshal(originator{Address: "127.0.0.1", Port: 6010})
	targetOpenedChannel := make(chan gossh.Channel, 1)
	targetOpenError := make(chan error, 1)
	go func() {
		for req := range targetSessionRequests {
			if req.Type != RequestType {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			go func() {
				channel, requests, openErr := target.server.OpenChannel(ChannelType, x11ExtraData)
				if openErr != nil {
					targetOpenError <- openErr
					return
				}
				go gossh.DiscardRequests(requests)
				targetOpenedChannel <- channel
			}()
		}
	}()

	ctx := context.WithValue(context.Background(), gliderssh.ContextKeyConn, origin.server)
	req := Request{
		SingleConnection: true,
		AuthProtocol:     "MIT-MAGIC-COOKIE-1",
		AuthCookie:       "0123456789abcdef",
		ScreenNumber:     0,
	}
	if err := Forward(ctx, target.client, targetSession, req); err != nil {
		t.Fatalf("Forward() error = %v", err)
	}

	var originNewChannel gossh.NewChannel
	select {
	case originNewChannel = <-originX11Channels:
	case err := <-targetOpenError:
		t.Fatalf("target OpenChannel() error = %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for origin X11 channel")
	}
	if string(originNewChannel.ExtraData()) != string(x11ExtraData) {
		t.Fatal("origin X11 channel extra data was not preserved")
	}
	originChannel, originRequests, err := originNewChannel.Accept()
	if err != nil {
		t.Fatalf("Accept origin X11 channel error = %v", err)
	}
	defer originChannel.Close()
	go gossh.DiscardRequests(originRequests)

	var targetChannel gossh.Channel
	select {
	case targetChannel = <-targetOpenedChannel:
	case err := <-targetOpenError:
		t.Fatalf("target OpenChannel() error = %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for target X11 channel")
	}
	defer targetChannel.Close()

	assertChannelData(t, targetChannel, originChannel, []byte("target-to-origin"))
	assertChannelData(t, originChannel, targetChannel, []byte("origin-to-target"))
}

func TestForwardReturnsErrorWhenTargetRejectsRequest(t *testing.T) {
	origin := newSSHPair(t)
	defer origin.close()
	go gossh.DiscardRequests(origin.serverReqs)

	target := newSSHPair(t)
	defer target.close()
	go gossh.DiscardRequests(target.serverReqs)
	targetSession, targetSessionChannel, requests := openSession(t, target)
	defer targetSession.Close()
	defer targetSessionChannel.Close()
	go func() {
		for req := range requests {
			_ = req.Reply(false, nil)
		}
	}()

	ctx := context.WithValue(context.Background(), gliderssh.ContextKeyConn, origin.server)
	err := Forward(ctx, target.client, targetSession, Request{})
	if !errors.Is(err, ErrRemoteForwardingRejected) {
		t.Fatalf("Forward() error = %v, want %v", err, ErrRemoteForwardingRejected)
	}
}

func openSession(t *testing.T, pair *sshPair) (*gossh.Session, gossh.Channel, <-chan *gossh.Request) {
	t.Helper()

	type sessionResult struct {
		session *gossh.Session
		err     error
	}
	resultCh := make(chan sessionResult, 1)
	go func() {
		session, err := pair.client.NewSession()
		resultCh <- sessionResult{session: session, err: err}
	}()

	newChannel := <-pair.serverChans
	if got := newChannel.ChannelType(); got != "session" {
		t.Fatalf("channel type = %q, want session", got)
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		t.Fatalf("Accept session error = %v", err)
	}
	result := <-resultCh
	if result.err != nil {
		_ = channel.Close()
		t.Fatalf("NewSession() error = %v", result.err)
	}
	return result.session, channel, requests
}

func assertChannelData(t *testing.T, writer io.Writer, reader io.Reader, want []byte) {
	t.Helper()
	writeErr := make(chan error, 1)
	go func() {
		_, err := writer.Write(want)
		writeErr <- err
	}()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("channel data = %q, want %q", got, want)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}
