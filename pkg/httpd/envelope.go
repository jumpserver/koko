package httpd

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	envelopeVersion byte = 0x01

	envelopeTerminalInput   byte = 0x01
	envelopeTerminalOutput  byte = 0x02
	envelopeTerminalCommand byte = 0x03
	envelopeError           byte = 0x04
	envelopeChat            byte = 0x05
	envelopeTerminalCreate  byte = 0x06
	envelopeTerminalClose   byte = 0x07

	envelopeHeaderSize     = 6
	envelopeTerminalIDSize = 4
	envelopeMaxPayload     = 10 * 1024 * 1024
)

var (
	errEnvelopeMalformed  = errors.New("malformed websocket envelope")
	errEnvelopeVersion    = errors.New("unsupported websocket envelope version")
	errEnvelopeTooLarge   = errors.New("websocket envelope payload too large")
	errTerminalIDRequired = errors.New("terminal id is required")
)

type envelope struct {
	Type    byte
	Payload []byte
}

type terminalCommandEnvelope struct {
	TerminalID uint32          `json:"terminalId,omitempty"`
	Command    string          `json:"command"`
	Params     json.RawMessage `json:"params,omitempty"`
	RequestID  string          `json:"requestId,omitempty"`
	Timestamp  int64           `json:"timestamp,omitempty"`
}

type terminalCreateEnvelope struct {
	RequestID string `json:"requestId"`
	Params    struct {
		Rows       int    `json:"rows"`
		Cols       int    `json:"cols"`
		Type       string `json:"type,omitempty"`
		Code       string `json:"code,omitempty"`
		Kubernetes struct {
			ID        string `json:"id,omitempty"`
			Namespace string `json:"namespace,omitempty"`
			Pod       string `json:"pod,omitempty"`
			Container string `json:"container,omitempty"`
		} `json:"kubernetes,omitempty"`
	} `json:"params"`
}

type terminalCloseEnvelope struct {
	TerminalID uint32 `json:"terminalId"`
	RequestID  string `json:"requestId,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type errorEnvelope struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	TerminalID uint32 `json:"terminalId,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

func parseEnvelope(data []byte) (envelope, error) {
	if len(data) < envelopeHeaderSize {
		return envelope{}, errEnvelopeMalformed
	}
	if data[0] != envelopeVersion {
		return envelope{}, errEnvelopeVersion
	}
	payloadLength := binary.BigEndian.Uint32(data[2:6])
	if payloadLength > envelopeMaxPayload {
		return envelope{}, errEnvelopeTooLarge
	}
	if uint32(len(data)-envelopeHeaderSize) != payloadLength {
		return envelope{}, errEnvelopeMalformed
	}
	return envelope{Type: data[1], Payload: data[envelopeHeaderSize:]}, nil
}

func buildEnvelope(messageType byte, payload []byte) ([]byte, error) {
	if len(payload) > envelopeMaxPayload {
		return nil, errEnvelopeTooLarge
	}
	result := make([]byte, envelopeHeaderSize+len(payload))
	result[0] = envelopeVersion
	result[1] = messageType
	binary.BigEndian.PutUint32(result[2:6], uint32(len(payload)))
	copy(result[envelopeHeaderSize:], payload)
	return result, nil
}

func parseTerminalEnvelopePayload(payload []byte) (uint32, []byte, error) {
	if len(payload) < envelopeTerminalIDSize {
		return 0, nil, errTerminalIDRequired
	}
	terminalID := binary.BigEndian.Uint32(payload[:envelopeTerminalIDSize])
	if terminalID == 0 {
		return 0, nil, errTerminalIDRequired
	}
	return terminalID, payload[envelopeTerminalIDSize:], nil
}

func buildTerminalOutputEnvelope(terminalID uint32, data []byte) ([]byte, error) {
	if terminalID == 0 {
		return nil, errTerminalIDRequired
	}
	payloadLength := envelopeTerminalIDSize + len(data)
	if payloadLength > envelopeMaxPayload {
		return nil, errEnvelopeTooLarge
	}
	result := make([]byte, envelopeHeaderSize+payloadLength)
	result[0] = envelopeVersion
	result[1] = envelopeTerminalOutput
	binary.BigEndian.PutUint32(result[2:envelopeHeaderSize], uint32(payloadLength))
	binary.BigEndian.PutUint32(result[envelopeHeaderSize:envelopeHeaderSize+envelopeTerminalIDSize], terminalID)
	copy(result[envelopeHeaderSize+envelopeTerminalIDSize:], data)
	return result, nil
}

func marshalEnvelopeJSON(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal websocket envelope payload: %w", err)
	}
	return payload, nil
}

func encodeMessageEnvelope(msg *Message) ([]byte, error) {
	switch msg.Type {
	case TerminalBinary, TerminalK8SBinary:
		return buildTerminalOutputEnvelope(msg.TerminalId, msg.Raw)
	case ChatMessage:
		return buildEnvelope(envelopeChat, []byte(msg.Data))
	case ERROR, TerminalError:
		payload, err := marshalEnvelopeJSON(errorEnvelope{
			Code: 500, Message: firstNonEmpty(msg.Err, msg.Data),
			TerminalID: msg.TerminalId, RequestID: msg.RequestId,
			Timestamp: time.Now().UnixMilli(),
		})
		if err != nil {
			return nil, err
		}
		return buildEnvelope(envelopeError, payload)
	case CLOSE, K8SClose:
		payload, err := marshalEnvelopeJSON(terminalCloseEnvelope{
			TerminalID: msg.TerminalId, RequestID: msg.RequestId, Reason: msg.Data,
		})
		if err != nil {
			return nil, err
		}
		return buildEnvelope(envelopeTerminalClose, payload)
	default:
		params, err := marshalEnvelopeJSON(msg)
		if err != nil {
			return nil, err
		}
		payload, err := marshalEnvelopeJSON(terminalCommandEnvelope{
			TerminalID: msg.TerminalId,
			Command:    msg.Type,
			Params:     params,
			RequestID:  msg.RequestId,
		})
		if err != nil {
			return nil, err
		}
		return buildEnvelope(envelopeTerminalCommand, payload)
	}
}

func decodeEnvelopeMessage(frame envelope) (*Message, error) {
	switch frame.Type {
	case envelopeTerminalInput:
		terminalID, data, err := parseTerminalEnvelopePayload(frame.Payload)
		if err != nil {
			return nil, err
		}
		return &Message{Type: TerminalBinary, TerminalId: terminalID, Raw: data}, nil
	case envelopeTerminalCommand:
		var command terminalCommandEnvelope
		if err := json.Unmarshal(frame.Payload, &command); err != nil {
			return nil, errEnvelopeMalformed
		}
		if command.Command == "" {
			return nil, errEnvelopeMalformed
		}
		var msg Message
		if len(command.Params) > 0 {
			if err := json.Unmarshal(command.Params, &msg); err != nil {
				return nil, errEnvelopeMalformed
			}
		}
		msg.Type = command.Command
		msg.TerminalId = command.TerminalID
		msg.RequestId = command.RequestID
		return &msg, nil
	case envelopeTerminalCreate:
		var request terminalCreateEnvelope
		if err := json.Unmarshal(frame.Payload, &request); err != nil {
			return nil, errEnvelopeMalformed
		}
		if request.RequestID == "" || request.Params.Rows <= 0 || request.Params.Cols <= 0 {
			return nil, errEnvelopeMalformed
		}
		return &Message{
			Type: TerminalCreate, Data: string(frame.Payload), RequestId: request.RequestID,
		}, nil
	case envelopeTerminalClose:
		var request terminalCloseEnvelope
		if err := json.Unmarshal(frame.Payload, &request); err != nil || request.TerminalID == 0 {
			return nil, errEnvelopeMalformed
		}
		return &Message{
			Type: CLOSE, TerminalId: request.TerminalID,
			RequestId: request.RequestID, Data: request.Reason,
		}, nil
	case envelopeChat:
		return &Message{Type: ChatMessage, Data: string(frame.Payload)}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported type 0x%02x", errEnvelopeMalformed, frame.Type)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "terminal error"
}
