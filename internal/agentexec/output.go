package agentexec

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type OutputStream struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// This cursor carries decoding state, never execution authority. Each lookup
// still asks Core to authorize the action's current user/client/org owner.
type outputCursor struct {
	Version int               `json:"v"`
	Action  string            `json:"action"`
	Offset  int64             `json:"offset"`
	Pending map[string][]byte `json:"pending,omitempty"`
}

func readOutputCursor(value, id string) (outputCursor, error) {
	var cursor outputCursor
	if len(value) > 512 {
		return cursor, errors.New("invalid output cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || !json.Valid(data) {
		return cursor, errors.New("invalid output cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Action != id || cursor.Offset < 0 || cursor.Offset > 1000000 || len(cursor.Pending) > 2 {
		return cursor, errors.New("invalid output cursor")
	}
	for stream, pending := range cursor.Pending {
		if (stream != "stdout" && stream != "stderr") || len(pending) > 3 || (len(pending) > 0 && utf8.FullRune(pending)) {
			return cursor, errors.New("invalid output cursor")
		}
	}
	return cursor, nil
}

func renderOutput(action *Action, cursor outputCursor, continuous bool) {
	if cursor.Pending == nil {
		cursor.Pending = make(map[string][]byte)
	}
	text := map[string]*strings.Builder{"stdout": {}, "stderr": {}}
	last := cursor.Offset
	for i := range action.Events {
		event := &action.Events[i]
		last = int64(event.Sequence)
		stream, _ := event.Payload["stream"].(string)
		value, ok := event.Payload["data"].(string)
		if !ok {
			continue
		}
		data := []byte(value)
		if event.Payload["encoding"] == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				action.OutputLossy = true
				continue
			}
			data = decoded
			// Preserve incomplete or binary bytes as base64. Converting only
			// valid chunks keeps legacy text consumers lossless too.
			if utf8.Valid(data) {
				event.Payload["data"], event.Payload["encoding"] = string(data), "utf-8"
			}
		}
		if text[stream] == nil {
			continue
		}
		data = append(cursor.Pending[stream], data...)
		for len(data) > 0 && utf8.FullRune(data) {
			char, width := utf8.DecodeRune(data)
			if char == utf8.RuneError && width == 1 {
				action.OutputLossy = true
			}
			text[stream].WriteRune(char)
			data = data[width:]
		}
		cursor.Pending[stream] = append([]byte(nil), data...)
	}
	finished := terminalState(action.Status) && !action.EvidencePending && action.NextOffset == nil
	if finished {
		for stream, pending := range cursor.Pending {
			if len(pending) > 0 {
				text[stream].WriteRune(utf8.RuneError)
				action.OutputLossy = true
			}
			delete(cursor.Pending, stream)
		}
	}
	if continuous {
		for _, stream := range []string{"stdout", "stderr"} {
			if text[stream].Len() > 0 {
				action.Output = append(action.Output, OutputStream{Stream: stream, Text: text[stream].String()})
			}
		}
	} else {
		action.OutputRequiresCursor = true
	}
	action.OutputComplete = finished && continuous
	if !finished {
		cursor.Version, cursor.Action, cursor.Offset = 1, action.ID, last
		data, _ := json.Marshal(cursor)
		action.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
}
