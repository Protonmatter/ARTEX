package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Match strings.TrimSpace, including Unicode whitespace, in the published schema.
const factWhitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000"

func factAssetIDSchema() map[string]any {
	return map[string]any{
		"type": []string{"integer", "string"}, "minimum": 1,
		"pattern": "^[" + factWhitespace + "]*\\+?0*[1-9][0-9]*[" + factWhitespace + "]*$",
	}
}

func factItemSchema() map[string]any {
	schema := obj(map[string]any{
		"summary":          str("Required: a non-empty one-sentence summary of this conclusion."),
		"intent_id":        idp("The intent that produced this fact. Omit to link it to the intent this worker is running."),
		"detail":           str("Details of this fact, including all related observations."),
		"evidence":         str("One line of key evidence: the command and the most proving output lines. Put longer output in detail."),
		"confidence":       str("observed (seen directly in output) | inferred (inferred from phenomena). Label negative conclusions honestly."),
		"source":           map[string]any{"type": "string", "enum": factSources, "description": "where the content came from: target_response | tool_output | third_party_intel | operator_input | inference (default when omitted)"},
		"asset_ids":        map[string]any{"type": "array", "items": factAssetIDSchema(), "description": "Asset IDs explicitly involved in this fact. Use asset_ids as the canonical field; tested_asset_ids is accepted as an alias. If both are provided, they must name the same set. Do not infer IDs from the intent."},
		"tested_asset_ids": map[string]any{"type": "array", "items": factAssetIDSchema(), "description": "Compatibility alias for asset_ids, not an additional list. When both are provided their sets must match. Only include assets actually involved in this conclusion."},
	}, "summary")
	summary := schema["properties"].(map[string]any)["summary"].(map[string]any)
	summary["minLength"] = 1
	summary["pattern"] = "[^" + factWhitespace + "]"
	schema["additionalProperties"] = false
	return schema
}

func recordFactSchema() map[string]any {
	schema := obj(map[string]any{"facts": map[string]any{
		"type": "array", "minItems": 1, "items": factItemSchema(),
		"description": "The facts to record, usually exactly one. Every item is a complete fact with its own summary. This is the only top-level property; put all fact fields inside its item.",
	}}, "facts")
	schema["additionalProperties"] = false
	return schema
}

// RepairRecordFactSchema publishes one facts-only shape for stored or cached
// schemas while retaining operator annotations on the facts array and its items.
// The handler continues to validate older shapes itself.
func RepairRecordFactSchema(stored map[string]any) map[string]any {
	out := recordFactSchema()
	facts := out["properties"].(map[string]any)["facts"].(map[string]any)
	storedProps, _ := stored["properties"].(map[string]any)
	storedFacts, _ := storedProps["facts"].(map[string]any)
	if storedFacts == nil {
		return out
	}
	if desc, ok := storedFacts["description"].(string); ok && strings.TrimSpace(desc) != "" {
		facts["description"] = desc
	}
	storedItems, _ := storedFacts["items"].(map[string]any)
	storedItemProps, _ := storedItems["properties"].(map[string]any)
	itemProps := facts["items"].(map[string]any)["properties"].(map[string]any)
	for name, raw := range itemProps {
		old, _ := storedItemProps[name].(map[string]any)
		prop := raw.(map[string]any)
		for _, annotation := range []string{"description", "default"} {
			if value, ok := old[annotation]; ok && value != nil {
				prop[annotation] = value
			}
		}
	}
	return out
}

type recordFactInput struct {
	Facts []factItem `json:"facts"`
	factItem
}

// Validate exact field names before decoding: encoding/json otherwise treats
// Summary, SUMMARY, etc. as aliases even with DisallowUnknownFields enabled.
func decodeRecordFactInput(in json.RawMessage) (recordFactInput, bool, error) {
	var out recordFactInput
	fields, err := exactFactFields(in, true)
	if err != nil {
		return out, false, err
	}
	_, batch := fields["facts"]
	if batch {
		for name := range fields {
			if name != "intent_id" && name != "facts" {
				return out, true, fmt.Errorf("field %q is not allowed at the top level in batch mode; only intent_id and facts are accepted", name)
			}
		}
		var items []json.RawMessage
		if err := json.Unmarshal(fields["facts"], &items); err != nil {
			return out, true, fmt.Errorf("facts must be an array: %w", err)
		}
		if len(items) == 0 {
			return out, true, fmt.Errorf("facts must be a non-empty array of complete facts, each with its own summary")
		}
		for i, item := range items {
			if _, err := exactFactFields(item, false); err != nil {
				return out, true, fmt.Errorf("facts[%d]: %w", i, err)
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(in))
	decoder.DisallowUnknownFields() // Also applies recursively to every factItem.
	if err := decoder.Decode(&out); err != nil {
		return out, batch, err
	}
	return out, batch, nil
}

func exactFactFields(in json.RawMessage, topLevel bool) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(in, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("record_fact input and each fact must be objects")
	}
	for name := range fields {
		switch name {
		case "summary", "detail", "evidence", "confidence", "source", "intent_id", "asset_ids", "tested_asset_ids":
		case "facts":
			if !topLevel {
				return nil, fmt.Errorf("unknown field %q", name)
			}
		default:
			return nil, fmt.Errorf("unknown field %q (field names are case-sensitive)", name)
		}
	}
	return fields, nil
}
