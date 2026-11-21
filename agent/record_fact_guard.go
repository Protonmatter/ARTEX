package agent

import (
	"context"
	"sync"
)

// A worker should not spend an unbounded number of turns retrying rejected
// record_fact calls. Successful and partially successful calls reset the count.
const maxConsecutiveRecordFactRefusals = 3

type recordFactGuardKey struct{}

type recordFactGuard struct {
	mu      sync.Mutex
	refused int
	cancel  context.CancelCauseFunc
}

func withRecordFactGuard(ctx context.Context) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	return context.WithValue(ctx, recordFactGuardKey{}, &recordFactGuard{cancel: cancel}), cancel
}

func noteRecordFactResult(ctx context.Context, fullyRefused bool) {
	guard, _ := ctx.Value(recordFactGuardKey{}).(*recordFactGuard)
	if guard == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if !fullyRefused {
		guard.refused = 0
		return
	}
	guard.refused++
	if guard.refused >= maxConsecutiveRecordFactRefusals {
		guard.cancel(AbortRepeatedRecordFactRefusal)
	}
}
