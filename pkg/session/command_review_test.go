package session

import (
	"context"
	"errors"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestPollCommandReviewTerminalStates(t *testing.T) {
	for _, terminal := range []model.LabelField{model.TicketApproved, model.TicketRejected, model.TicketClosed} {
		calls := 0
		result, err := PollCommandReview(func() (model.TicketState, error) {
			calls++
			if calls > 2 {
				t.Fatal("polled after terminal state")
			}
			if calls == 1 {
				return model.TicketState{State: model.TicketOpen}, nil
			}
			return model.TicketState{State: terminal, Processor: "reviewer"}, nil
		}, nil)
		if err != nil || result.State != terminal || result.Processor != "reviewer" {
			t.Fatalf("terminal %s: %+v, %v", terminal, result, err)
		}
	}
}

func TestPollCommandReviewRetryPolicy(t *testing.T) {
	requestErr := errors.New("request failed")
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"unknown stops", nil, false},
		{"unknown retries", nil, true},
		{"request stops", requestErr, false},
		{"request retries", requestErr, true},
		{"cancellation stops", context.Canceled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := PollCommandReview(func() (model.TicketState, error) {
				calls++
				if calls > 2 {
					t.Fatal("polled after terminal state")
				}
				if calls == 1 {
					return model.TicketState{State: "unknown"}, tc.err
				}
				return model.TicketState{State: model.TicketApproved}, nil
			}, func(_ model.TicketState, err error) bool {
				return tc.retry && !errors.Is(err, context.Canceled)
			})
			wantErr := tc.err
			if wantErr == nil {
				wantErr = ErrUnknownCommandReviewState
			}
			if tc.retry && !errors.Is(tc.err, context.Canceled) {
				wantErr = nil
			}
			if !errors.Is(err, wantErr) || (calls == 2) != (wantErr == nil) {
				t.Fatalf("calls=%d, err=%v; want %v", calls, err, wantErr)
			}
		})
	}
}
