package session

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

// NewConnectionInfo assigns the Core identity before any remote creation, so
// callers can track and compensate a creation whose response is lost.
func NewConnectionInfo(token *model.ConnectToken, remoteAddr string, loginFrom, kind model.LabelField) (model.Session, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return model.Session{}, errors.New("secure_random_unavailable")
	}
	info := token.CreateSession(remoteAddr, loginFrom, kind)
	info.ID = id.String()
	return info, nil
}

// BindConnection attaches the connection's login approval and required face
// monitor to an existing Core session. Ticket association remains best effort,
// while failure to join required monitoring must stop the connection. The caller
// owns session registration and cleanup, including durable cleanup when needed.
func BindConnection(client *service.JMService, sessionID string, token *model.ConnectToken, onTicketError func(error)) error {
	if token.Ticket != nil {
		if err := client.CreateSessionTicketRelation(sessionID, token.Ticket.ID); err != nil && onTicketError != nil {
			onTicketError(err)
		}
	}
	if token.FaceMonitorToken == "" {
		return nil
	}
	return client.JoinFaceMonitor(service.JoinFaceMonitorRequest{
		FaceMonitorToken: token.FaceMonitorToken,
		SessionId:        sessionID,
	})
}
