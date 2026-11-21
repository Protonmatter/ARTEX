package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	actool "github.com/Autumn-27/norma/tool"
	"github.com/Protonmatter/ReconRivet/agent"
)

func TestRecordFactInputSchemaUpgrade(t *testing.T) {
	skipWithoutDSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	t.Cleanup(func() { agent.SetToolResolve(nil) })
	pg := m.pg
	old, err := pg.GetTool("record_fact")
	if err != nil || old == nil {
		t.Fatal("missing record_fact", err)
	}
	bindings, _ := json.Marshal(old.Agents)
	t.Cleanup(func() { pg.UpdateTool(old.Key, old.Description, old.Schema, bindings, old.Enabled) })
	legacy := map[string]any{
		"type": "object", "required": []string{"summary"},
		"anyOf": []any{map[string]any{"required": []string{"summary"}}},
		"properties": map[string]any{
			"summary":    map[string]any{"type": "string", "description": "operator summary"},
			"confidence": map[string]any{"type": "string", "default": "inferred"},
			"custom":     map[string]any{"type": "string"},
			"facts":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
	}
	raw, _ := json.Marshal(legacy)
	if err := pg.UpdateTool("record_fact", "operator tool", raw, bindings, old.Enabled); err != nil {
		t.Fatal(err)
	}
	check := func(schema map[string]any) {
		t.Helper()
		rawRequired, _ := json.Marshal(schema["required"])
		var required []string
		if err := json.Unmarshal(rawRequired, &required); err != nil || len(required) != 1 || required[0] != "facts" {
			t.Fatalf("facts-only contract not required: %#v", schema["required"])
		}
		if _, ok := schema["anyOf"]; ok {
			t.Fatal("root anyOf still present")
		}
		if schema["additionalProperties"] != false {
			t.Fatal("unknown fields allowed")
		}
		props := schema["properties"].(map[string]any)
		items := props["facts"].(map[string]any)["items"].(map[string]any)
		itemProps := items["properties"].(map[string]any)
		if len(props) != 1 || props["facts"] == nil || itemProps["tested_asset_ids"] == nil || itemProps["summary"] == nil {
			t.Fatal("repaired schema must publish facts only with complete items")
		}
		if props["summary"] != nil || props["confidence"] != nil || props["custom"] != nil {
			t.Fatal("obsolete top-level fact fields remain published")
		}
		if err := actool.ValidateInput(schema, json.RawMessage(`{"facts":[{"summary":"complete batch item"}]}`)); err != nil {
			t.Fatal("valid batch rejected", err)
		}
	}
	// Runtime repair protects cached or edited rows even before startup migration.
	tools := agent.ToolResolve()(context.Background(), "worker", agent.NewToolSet(nil, "").WorkerTools())
	found := false
	for _, tool := range tools {
		if tool.Name() == "record_fact" {
			check(tool.InputSchema())
			found = true
		}
	}
	if !found {
		t.Fatal("record_fact not resolved")
	}
	s.seedFactInputSchema()
	repaired, err := pg.GetTool("record_fact")
	if err != nil || repaired == nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(repaired.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	check(schema)
	if repaired.Description != "operator tool" || repaired.Enabled != old.Enabled {
		t.Fatal("operator tool settings changed")
	}
	var before, after time.Time
	if err := pg.QueryRow("SELECT updated_at FROM tools WHERE key='record_fact'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.seedFactInputSchema()
	if err := pg.QueryRow("SELECT updated_at FROM tools WHERE key='record_fact'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) {
		t.Fatal("second migration changed the row")
	}
}
