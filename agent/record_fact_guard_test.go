package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRecordFactRefusalGuardResetsOnWrite(t *testing.T) {
	ctx, cancel := withRecordFactGuard(context.Background())
	defer cancel(nil)
	for i := 0; i < maxConsecutiveRecordFactRefusals-1; i++ {
		noteRecordFactResult(ctx, true)
	}
	if ctx.Err() != nil {
		t.Fatal("guard stopped before the refusal limit")
	}
	noteRecordFactResult(ctx, false)
	for i := 0; i < maxConsecutiveRecordFactRefusals-1; i++ {
		noteRecordFactResult(ctx, true)
	}
	if ctx.Err() != nil {
		t.Fatal("successful write did not reset the refusal count")
	}
	noteRecordFactResult(ctx, true)
	if context.Cause(ctx) != AbortRepeatedRecordFactRefusal {
		t.Fatalf("wrong guard cause: %v", context.Cause(ctx))
	}
}

func TestRecordFactRefusalGuardStopsMalformedCalls(t *testing.T) {
	store := guardTaskStore(t)
	ctx, cancel := withRecordFactGuard(context.Background())
	defer cancel(nil)
	tool := NewToolSet(store, "worker").recordFact()
	for i := 0; i < maxConsecutiveRecordFactRefusals; i++ {
		result, err := tool.Call(ctx, json.RawMessage(`{"summary":"invalid beside facts","facts":[{"summary":"valid item"}]}`), nil)
		if err != nil || !result.IsError {
			t.Fatalf("malformed call %d was not refused: result=%v err=%v", i+1, result, err)
		}
		if i < maxConsecutiveRecordFactRefusals-1 && ctx.Err() != nil {
			t.Fatalf("guard stopped after only %d refusals", i+1)
		}
	}
	if context.Cause(ctx) != AbortRepeatedRecordFactRefusal {
		t.Fatalf("repeated malformed calls did not stop the worker: %v", context.Cause(ctx))
	}
}
