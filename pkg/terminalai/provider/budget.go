package provider

import (
	"context"
	"errors"
	"sync/atomic"
)

var ErrRequestBudget = errors.New("terminal AI model request budget exhausted")

type requestBudget struct {
	limit int64
	used  atomic.Int64
}

type requestBudgetKey struct{}

func WithRequestBudget(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, requestBudgetKey{}, &requestBudget{limit: int64(limit)})
}

func ConsumeRequest(ctx context.Context) error {
	budget, _ := ctx.Value(requestBudgetKey{}).(*requestBudget)
	if budget == nil {
		return nil
	}
	if budget.used.Add(1) <= budget.limit {
		return nil
	}
	return ErrRequestBudget
}

func RequestUsage(ctx context.Context) int {
	budget, _ := ctx.Value(requestBudgetKey{}).(*requestBudget)
	if budget == nil {
		return 0
	}
	return int(budget.used.Load())
}
