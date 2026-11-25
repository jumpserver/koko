package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func (v *Server) events(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	var after uint64
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		var err error
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(w, 400, "invalid_event_cursor", "Invalid event cursor")
			return
		}
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	if len(s.subscribers) >= 4 {
		s.mu.Unlock()
		fail(w, 429, "subscriber_limit", "Too many event subscriptions")
		return
	}
	s.nextSubscriber++
	id := s.nextSubscriber
	ch := make(chan Event, 64)
	s.subscribers[id] = ch
	initial := []Event{}
	if after == 0 || after > s.sequence || (len(s.history) > 0 && after < s.history[0].Sequence-1) {
		executions := []ExecutionView{}
		for _, e := range s.executions {
			executions = append(executions, e.snapshot())
		}
		consoles := []ConsoleView{}
		for _, c := range s.consoles {
			consoles = append(consoles, c.view())
		}
		sort.Slice(consoles, func(i, j int) bool { return consoles[i].ID < consoles[j].ID })
		sort.Slice(executions, func(i, j int) bool { return executions[i].ID < executions[j].ID })
		initial = append(initial, Event{s.sequence, "reset", map[string]any{"session": s.viewLocked(), "consoles": consoles, "executions": executions}})
	} else {
		for _, event := range s.history {
			if event.Sequence > after {
				initial = append(initial, event)
			}
		}
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, id); s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	controller := http.NewResponseController(w)
	// Establish an empty resumed stream immediately; otherwise a reconnect
	// with no missed events waits until the first heartbeat to receive headers.
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if controller.Flush() != nil {
		return
	}
	send := func(event Event) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, data); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	closedSent := false
	deliver := func(event Event) bool {
		if event.Type == "ai.message" && s.grant.Check() != nil {
			return true
		}
		if event.Type == "session.closed" {
			closedSent = true
		}
		return send(event)
	}
	for _, event := range initial {
		if !deliver(event) {
			return
		}
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			// Session cancellation and buffered events can become ready together.
			// Flush the final close event instead of randomly losing it in select.
			for {
				select {
				case event, ok := <-ch:
					if !ok {
						goto drained
					}
					if !deliver(event) {
						return
					}
				default:
					goto drained
				}
			}
		drained:
			if !closedSent {
				s.mu.Lock()
				last := Event{s.sequence, "session.closed", map[string]string{"reason": "server_shutdown"}}
				for i := len(s.history) - 1; i >= 0; i-- {
					if s.history[i].Type == "session.closed" {
						last = s.history[i]
						break
					}
				}
				s.mu.Unlock()
				_ = deliver(last)
			}
			return
		case event, ok := <-ch:
			if !ok || !deliver(event) {
				return
			}
		case <-ticker.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		}
	}
}
