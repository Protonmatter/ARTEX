package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Protonmatter/ReconRivet/db"
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

func TestWorkerStopsRepeatedRecordFactRefusals(t *testing.T) {
	store := guardTaskStore(t)
	payload := map[string]any{"summary": "inspect a lab service"}
	intentID := addGuardNode(t, store, db.KindIntent, payload, "running")
	rawIntent, _ := json.Marshal(payload)
	var calls atomic.Int64
	provider := captureUsageProvider{stream: func(_ context.Context, yield func(llm.StreamEvent, error) bool) {
		callID := fmt.Sprintf("fact-%d", calls.Add(1))
		for _, event := range []llm.StreamEvent{
			{Type: llm.SEToolUseStart, ToolID: callID, ToolName: "record_fact"},
			{Type: llm.SEToolInputJSON, Text: `{"summary":"invalid beside facts","facts":[{"summary":"lab observation"}]}`},
			{Type: llm.SEMessageDelta, StopReason: "tool_use"},
		} {
			if !yield(event, nil) {
				return
			}
		}
	}}
	worker := NewWorker(provider, "fixture", t.TempDir(), nil, 0, 10)
	_, writes, err := worker.Execute(t.Context(), "worker", 0, nil, store, &db.Node{ID: intentID, Payload: rawIntent}, nil, nil, nil, nil)
	if err != AbortRepeatedRecordFactRefusal {
		t.Fatalf("worker error=%v, want refusal limit", err)
	}
	if got := calls.Load(); got != maxConsecutiveRecordFactRefusals || writes.Facts != 0 {
		t.Fatalf("worker made %d provider calls and wrote %+v, want %d calls and no facts", got, writes, maxConsecutiveRecordFactRefusals)
	}
}

func TestWorkerWritesFactsOnlyContract(t *testing.T) {
	store := guardTaskStore(t)
	payload := map[string]any{"summary": "inspect a lab service"}
	intentID := addGuardNode(t, store, db.KindIntent, payload, "running")
	rawIntent, _ := json.Marshal(payload)
	provider := toolCallThenStop("record_fact", `{"facts":[{"summary":"lab response observed","evidence":"HTTP 200 with expected marker","confidence":"observed","source":"tool_output"}]}`)
	worker := NewWorker(provider, "fixture", t.TempDir(), nil, 0, 3)
	_, writes, err := worker.Execute(t.Context(), "worker", 0, nil, store, &db.Node{ID: intentID, Payload: rawIntent}, nil, nil, nil, nil)
	if err != nil || writes.Facts != 1 {
		t.Fatalf("facts-only worker run: writes=%+v err=%v", writes, err)
	}
	yielded, err := store.FactsYielded(intentID)
	if err != nil || len(yielded) != 1 {
		t.Fatalf("worker fact missing from its intent: yielded=%v err=%v", yielded, err)
	}
}
