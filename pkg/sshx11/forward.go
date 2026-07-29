package sshx11

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
)

var (
	ErrClientConnectionUnavailable = errors.New("SSH client connection is unavailable for X11 forwarding")
	ErrChannelHandlerRegistered    = errors.New("SSH X11 channel handler is already registered")
	ErrRemoteForwardingRejected    = errors.New("target SSH server rejected X11 forwarding")
)

// Forward requests X11 forwarding on the target session and bridges every X11
// channel opened by the target back to the original OpenSSH client connection.
//
// The target client must be dedicated to this originating client. SSH X11
// channels do not identify the session request that created them, so sharing a
// target SSH transport could route graphical traffic to the wrong user.
func Forward(ctx context.Context, targetClient *gossh.Client, targetSession *gossh.Session,
	req Request) error {
	clientConn, ok := ClientConnectionFromContext(ctx)
	if !ok {
		return ErrClientConnectionUnavailable
	}

	channels := targetClient.HandleChannelOpen(ChannelType)
	if channels == nil {
		return ErrChannelHandlerRegistered
	}

	ok, err := targetSession.SendRequest(RequestType, true, req.Marshal())
	if err != nil {
		go rejectChannels(ctx, channels)
		return fmt.Errorf("request target X11 forwarding: %w", err)
	}
	if !ok {
		go rejectChannels(ctx, channels)
		return ErrRemoteForwardingRejected
	}

	go serveChannels(ctx, clientConn, channels)
	return nil
}

func rejectChannels(ctx context.Context, channels <-chan gossh.NewChannel) {
	for {
		select {
		case <-ctx.Done():
			return
		case newChannel, ok := <-channels:
			if !ok {
				return
			}
			_ = newChannel.Reject(gossh.Prohibited, "X11 forwarding is not active")
		}
	}
}

func serveChannels(ctx context.Context, clientConn gossh.Conn, channels <-chan gossh.NewChannel) {
	for {
		select {
		case <-ctx.Done():
			return
		case newChannel, ok := <-channels:
			if !ok {
				return
			}
			go bridgeChannel(clientConn, newChannel)
		}
	}
}

func bridgeChannel(clientConn gossh.Conn, targetNewChannel gossh.NewChannel) {
	clientChannel, clientRequests, err := clientConn.OpenChannel(
		ChannelType, targetNewChannel.ExtraData())
	if err != nil {
		_ = targetNewChannel.Reject(gossh.ConnectionFailed, "open client X11 channel failed")
		logger.Errorf("Open SSH client X11 channel failed: %s", err)
		return
	}

	targetChannel, targetRequests, err := targetNewChannel.Accept()
	if err != nil {
		_ = clientChannel.Close()
		logger.Errorf("Accept target SSH X11 channel failed: %s", err)
		return
	}

	go gossh.DiscardRequests(clientRequests)
	go gossh.DiscardRequests(targetRequests)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(clientChannel, targetChannel)
		_ = clientChannel.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(targetChannel, clientChannel)
		_ = targetChannel.CloseWrite()
	}()
	wg.Wait()
	_ = clientChannel.Close()
	_ = targetChannel.Close()
}
