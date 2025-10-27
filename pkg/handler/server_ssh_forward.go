package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver/koko/pkg/srvconn"
	gossh "golang.org/x/crypto/ssh"

	modelCommon "github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

const (
	ChannelTCPIPForward       = "tcpip-forward"
	ChannelCancelTCPIPForward = "cancel-tcpip-forward"
	ChannelForwardedTCPIP     = "forwarded-tcpip"
)

func (s *Server) ReversePortForwardingPermission(ctx ssh.Context, dstHost string, dstPort uint32) bool {
	logger.Debugf("Reverse Port Forwarding: %s %s %d", ctx.User(), dstHost, dstPort)
	return config.GlobalConfig.EnableReversePortForward
}

func (s *Server) HandleSSHRequest(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	reqId, ok := ctx.Value(ctxID).(string)
	if !ok {
		logger.Errorf("cannot get request id from context")
		return false, []byte("port forwarding is disabled")
	}
	switch req.Type {
	case ChannelTCPIPForward:
		var reqPayload remoteForwardRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			logger.Errorf("parse tcpip-forward request failed: %s", err.Error())
			return false, []byte{}
		}
		if srv.ReversePortForwardingCallback == nil || !srv.ReversePortForwardingCallback(ctx, reqPayload.BindAddr, reqPayload.BindPort) {
			return false, []byte("port forwarding is disabled")
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))

		vsReq := s.getVSCodeReq(reqId)
		if vsReq == nil {
			user := ctx.Value(auth.ContextKeyUser).(*model.User)
			directReq := ctx.Value(auth.ContextKeyDirectLoginFormat)
			directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq)
			if !ok3 {
				return false, []byte("port forwarding is disabled, must be direct login request")
			}
			var tokenInfo *model.ConnectToken
			var err error
			if directRequest.IsToken() {
				// connection token 的方式使用 vscode 连接
				tokenInfo = directRequest.ConnectToken
				matchedProtocol := tokenInfo.Protocol == model.ProtocolSSH
				assetSupportedSSH := tokenInfo.Asset.IsSupportProtocol(model.ProtocolSSH)
				if !matchedProtocol || !assetSupportedSSH {
					msg := "not ssh asset connection token"
					logger.Errorf("ide support failed: %s", msg)
					return false, []byte(msg)
				}
			} else {
				tokenInfo, err = s.buildConnectToken(ctx, user, directRequest)
				if err != nil {
					msg := "cannot build connect token"
					logger.Errorf("ide supoort failed, err:%s", err.Error())
					return false, []byte(msg)
				}
			}
			sshClient, err1 := s.buildSSHClient(tokenInfo)
			if err1 != nil {
				msg := "cannot build ssh client"
				logger.Errorf("ide support failed: %s", msg)
				return false, []byte(msg)
			}
			host, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
			reqSession := tokenInfo.CreateSession(host, model.LoginFromSSH, model.TUNNELType)
			respSession, err := s.jmsService.CreateSession(reqSession)
			if err != nil {
				logger.Errorf("Create reverse port tunnel session err: %s", err)
				return false, []byte("cannot create tunnel session")
			}
			childCtx, cancel := context.WithCancel(ctx)
			traceSession := session.NewSession(&respSession, func(task *model.TerminalTask) error {
				switch task.Name {
				case model.TaskKillSession:
					cancel()
					logger.Info("ide session  killed as task kill session")
					return nil
				case model.TaskPermExpired:
					cancel()
					logger.Info("ide session killed as task perm expired")
					return nil
				case model.TaskPermValid:
					return nil
				}
				return fmt.Errorf("ssh proxy not support task: %s", task.Name)
			})
			session.AddSession(traceSession)
			defer func() {
				if _, err2 := s.jmsService.SessionFinished(respSession.ID, modelCommon.NewNowUTCTime()); err2 != nil {
					logger.Errorf("Finish tunnel session err: %s", err2)
				}
				session.RemoveSession(traceSession)
			}()
			s.recordSessionLifecycle(respSession.ID, model.AssetConnectSuccess, "")
			vsReq = &vscodeReq{
				reqId:    reqId,
				user:     user,
				client:   sshClient,
				forwards: make(map[string]net.Listener),
			}
			go func() {
				s.addVSCodeReq(vsReq)
				defer s.deleteVSCodeReq(vsReq)
				<-childCtx.Done()
				if sshClient.Reused || sshClient.KeyId != "" {
					srvconn.ReleaseClientCacheKey(sshClient.KeyId, sshClient)
				} else {
					_ = sshClient.Close()
				}
				logger.Info("ide client removed, all alive forward will be closed by default")
				if _, err2 := s.jmsService.SessionFinished(respSession.ID, modelCommon.NewNowUTCTime()); err2 != nil {
					logger.Errorf("Create tunnel session err: %s", err2)
				}
				session.RemoveSession(traceSession)
				s.recordSessionLifecycle(respSession.ID, model.AssetConnectFinished, "")
			}()
		}

		ln, err := vsReq.client.Listen("tcp", addr)
		if err != nil {
			logger.Errorf("port forwarding listen failed: %s", err.Error())
			return false, []byte("port forwarding is failed, cannot listen tcp")
		}
		go func() {
			vsReq.AddForward(addr, ln)
			defer vsReq.RemoveForward(addr)
			<-ctx.Done()
			logger.Info("ide port forward removed")
		}()
		_, destPortStr, _ := net.SplitHostPort(ln.Addr().String())
		destPort, _ := strconv.Atoi(destPortStr)
		go func() {
			for {
				c, err2 := ln.Accept()
				if err2 != nil {
					if err2 != io.EOF {
						logger.Errorf("accept failed: %s", err2.Error())
					} else {
						logger.Infof("accept failed: %s", err2.Error())
					}
					break
				}
				originAddr, orignPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
				originPort, _ := strconv.Atoi(orignPortStr)
				payload := gossh.Marshal(&remoteForwardChannelData{
					DestAddr:   reqPayload.BindAddr,
					DestPort:   uint32(destPort),
					OriginAddr: originAddr,
					OriginPort: uint32(originPort),
				})
				conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
				go func() {
					ch, reqs, err1 := conn.OpenChannel(ChannelForwardedTCPIP, payload)
					if err1 != nil {
						logger.Errorf("open forwarded-tcpip channel failed: %s", err1.Error())
						return
					}
					go gossh.DiscardRequests(reqs)
					go func() {
						defer func() {
							_ = ch.Close()
							_ = c.Close()
						}()
						_, _ = io.Copy(ch, c)
					}()
					go func() {
						defer func() {
							_ = ch.Close()
							_ = c.Close()
						}()
						_, _ = io.Copy(c, ch)
					}()
				}()
			}
		}()
		return true, gossh.Marshal(&remoteForwardSuccess{uint32(destPort)})

	case ChannelCancelTCPIPForward:
		vsReq := s.getVSCodeReq(reqId)
		if vsReq == nil {
			return false, []byte("port forwarding is disabled, cannot found alive connection")
		}
		var reqPayload remoteForwardCancelRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			logger.Errorf("parse cancel-tcpip-forward request failed: %s", err.Error())
			return false, []byte{}
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		ln := vsReq.GetForward(addr)
		if ln != nil {
			_ = ln.Close()
			vsReq.RemoveForward(addr)
		}
		return true, nil
	default:
		return false, nil
	}
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}
