package engine

import (
	"context"
	"time"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

const (
	catalogSocketReadTimeout = 2 * time.Hour
	catalogInspectionTimeout = 12 * time.Second
)

// Resource tree reads live as long as both the HTTP request and the session.
// AI metadata shares the pool but has its own time budget.
func catalogQueryContext(request, session context.Context, operation string) (context.Context, context.CancelFunc) {
	var ctx context.Context
	var cancel context.CancelFunc
	if operation == "ai" {
		ctx, cancel = context.WithTimeout(request, catalogInspectionTimeout)
	} else {
		ctx, cancel = context.WithCancel(sqlservice.WithCatalogRequestLifetime(request))
	}
	stop := context.AfterFunc(session, cancel)
	return ctx, func() { stop(); cancel() }
}

func catalogQueueTimeout(operation string) time.Duration {
	switch operation {
	case "inspect", "object-detail", "ai", "overview", "diagram":
		return catalogInspectionTimeout
	case "ddl", "object-definition":
		return 30 * time.Second
	default:
		return 3 * time.Second
	}
}

// Acquire the session slot first so one session's backlog cannot occupy the
// global pool. The two waits share one deadline.
func acquireCatalogSlots(ctx context.Context, operation string, session, global chan struct{}) (release func(), err error) {
	wait, cancel := context.WithTimeout(ctx, catalogQueueTimeout(operation))
	defer cancel()
	select {
	case session <- struct{}{}:
	case <-wait.Done():
		return nil, wait.Err()
	}
	select {
	case global <- struct{}{}:
	case <-wait.Done():
		<-session
		return nil, wait.Err()
	}
	release = func() { <-global; <-session }
	if err := wait.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
