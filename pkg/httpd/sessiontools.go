package httpd

import (
	"encoding/json"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func agentContextSnapshot(
	token *model.ConnectToken,
	sessionKind string,
) agentapi.ContextSnapshot {
	if token == nil {
		return agentapi.ContextSnapshot{SessionKind: sessionKind}
	}
	protocol := strings.ToLower(strings.TrimSpace(token.Protocol))
	commandLanguage := protocol
	dialect := ""
	switch protocol {
	case srvconn.ProtocolSSH, srvconn.ProtocolTELNET, srvconn.ProtocolK8s:
		commandLanguage = "shell"
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
		commandLanguage = "sql"
		dialect = protocol
	case srvconn.ProtocolSFTP:
		commandLanguage = "sftp"
	}
	if sessionKind == "file" {
		commandLanguage = "sftp"
	}
	return agentapi.ContextSnapshot{
		SessionKind: sessionKind, InteractionMode: "live",
		CommandLanguage: commandLanguage, Dialect: dialect,
		Protocol: protocol,
		ConnectionMethod: firstNonEmpty(
			token.ConnectMethod.Value,
			token.ConnectMethod.Type,
			token.ConnectMethod.Component,
		),
		AssetID: token.Asset.ID, AssetName: token.Asset.Name,
		AssetAddress: token.Asset.Address, PlatformID: token.Platform.ID,
		PlatformCategory: firstNonEmpty(
			token.Platform.Category.Value, token.Platform.Category.Label,
		),
		PlatformType: firstNonEmpty(
			token.Platform.Type.Value, token.Platform.Type.Label,
		),
		PlatformName: token.Platform.Name, BaseOS: token.Platform.BaseOs,
		Charset: firstNonEmpty(
			token.Platform.Charset.Value, token.Platform.Charset.Label,
		),
		Database: token.Asset.SpecInfo.DBName,
	}
}

func sendMCPFrameError(ws *UserWebsocket, message *Message, value error) {
	if ws == nil || message == nil || value == nil {
		return
	}
	var request struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(message.Data), &request)
	response := sessiontools.MCPResponse{
		JSONRPC: "2.0", ID: request.ID,
		Error: &sessiontools.MCPRPCError{Code: -32602, Message: value.Error()},
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return
	}
	responseType := MCPResponse
	if message.Type == MCPCancel {
		responseType = MCPCancelResult
	}
	ws.SendMessage(&Message{
		Id: ws.Uuid, Type: responseType,
		Version:           sessiontools.MCPProtocolVersion,
		ResourceSessionID: message.ResourceSessionID,
		TerminalId:        message.TerminalId, Data: string(payload),
	})
}

func (h *webSftp) sendMCPError(message *Message, value error) {
	sendMCPFrameError(h.ws, message, value)
}
