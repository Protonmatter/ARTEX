# LLM Context Size Telemetry (P3.5 Phase A)

`RECONRIVET_CONTEXT_TELEMETRY=1` enables opt-in, size-only per-call telemetry for every `llmrec` `Stream` and `Complete` call. It is **disabled by default**: when off, no request sizing runs and no rows are written to PostgreSQL.

This phase measures context composition without changing any prompt, compaction policy, or model behavior, so Phases B (`RECONRIVET_BOUNDED_CONTEXT`) and C (`RECONRIVET_CACHE_PREFIX` / `RECONRIVET_COMPACTION_MEMO`) can be evaluated against real numbers first.

---

## Configuration

| Environment variable | Values | Default |
|---|---|---|
| `RECONRIVET_CONTEXT_TELEMETRY` | `1`, `true`, `on`, `yes` (case-insensitive) | off |

Telemetry runs independently of `llm_record` (`recordBodies`): enabling `RECONRIVET_CONTEXT_TELEMETRY=1` works even when full request/response body recording is disabled, and stores **no prompt, tool, or message content** — only byte lengths, counts, token usage, and metadata.

### Stopping, retention, and write cost

When `RECONRIVET_CONTEXT_TELEMETRY=1` is active, each LLM call incurs one lightweight `INSERT` into `llm_context` (~200 bytes/row plus index entries, no bodies), so the table grows by one row per completion call. Unset `RECONRIVET_CONTEXT_TELEMETRY` (and restart) once the measurement campaign is finished to stop all writes and sizing work, or purge old rows periodically:

```sql
DELETE FROM llm_context WHERE ts < now() - interval '30 days';
```

---

## Measurement Semantics (`llmrec.MeasureContext`)

Every enabled `Stream` and `Complete` call measures the assembled `llm.CompletionRequest` right before it is handed to the inner provider, and persists one row to `llm_context` (migration `0009_llm_context.sql`) on completion or error:

- **`system_static_bytes` & `system_dynamic_bytes`**:
  - `norma`'s `CompletionRequest.System` is a slice of segments that `llm.joinSystem` trims (`strings.TrimSpace`), drops when empty, and joins with blank lines (`"\n\n"`). `MeasureContext` applies the exact same trim, drop-empty, and `"\n\n"` join rules per slice.
  - When `DynamicBoundary > 0`, `System` is split at `b = min(DynamicBoundary, len(System))`: `System[:b]` is measured into `system_static_bytes` and `System[b:]` into `system_dynamic_bytes`.
  - When `DynamicBoundary <= 0`, there is no provider-visible cache split, so the entire `System` slice is measured as one block in `system_static_bytes` and `system_dynamic_bytes` is `0`.
  - **Why this choice**: `agent/deferred.go` returns `boundary = 0` when no global deferred-MCP block exists and `boundary = len(system)` when one does. Placing the single unsplit block in `system_static_bytes` ensures `system_static_bytes + system_dynamic_bytes` always equals total system bytes and `system_dynamic_bytes > 0` strictly signals a dynamic suffix placed after a cache breakpoint.
- **`tool_count` & `tools_bytes`**: `len(req.Tools)` and `len(json.Marshal(req.Tools))` (`0` when no tools). Go's `encoding/json` sorts map keys deterministically.
- **`message_count`, `messages_user_bytes`, `messages_assistant_bytes`, `messages_other_bytes`, `last_message_bytes`**: each `llm.Message` is measured by `len(json.Marshal(msg))` and accumulated by role (`user`, `assistant`, or other); `last_message_bytes` is the serialized byte size of the final message.
- **`tool_result_bytes`**: serialized byte length of `tool_result` content blocks inside messages. Reported as an **informational subset** of `messages_user_bytes` (not added to the partition denominator, avoiding double counting).

### Pro-rata token attribution

Rather than shipping per-provider tokenizers, each section's share of real `input_tokens` is computed per call by byte proportion:

```text
total_bytes(call) = system_static_bytes + system_dynamic_bytes + tools_bytes
                  + messages_user_bytes + messages_assistant_bytes + messages_other_bytes

share(section) = SUM(input_tokens * (section_bytes / total_bytes))
               / SUM(input_tokens)          -- over calls with total_bytes > 0 and input_tokens > 0
```

The six partition shares sum to `1.0`. `cache_read_ratio = SUM(cache_read) / NULLIF(SUM(input_tokens), 0)` (in ReconRivet/Norma, `input_tokens` already includes cached tokens).

---

## Querying & Aggregation

### HTTP endpoint (admin JWT required, no UI in Phase A)

```text
GET /api/llm/context/summary?days=30&top=20&task=<optional-task-id>
```

Returns `enabled`, `by_worker_model` (calls, totals, `cache_read_ratio`, `input_p50`/`p90`/`p99`/`max`, avg tool/message/last-message size, and pro-rata `shares`), and `top_tasks` ordered by total `input_tokens`.

### Standalone SQL query (`psql`)

```sql
WITH base AS (
    SELECT *,
           (system_static_bytes + system_dynamic_bytes + tools_bytes
            + messages_user_bytes + messages_assistant_bytes + messages_other_bytes) AS total_bytes
    FROM llm_context
    WHERE ts >= now() - interval '30 days'
)
SELECT COALESCE(NULLIF(worker,''), '(unknown)') AS worker,
       COALESCE(NULLIF(model,''), '(unknown)')  AS model,
       COUNT(*)                                  AS calls,
       SUM(input_tokens)                         AS input_tokens,
       ROUND((SUM(cache_read)::numeric / NULLIF(SUM(input_tokens), 0))::numeric, 3) AS cache_read_ratio,
       PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY input_tokens) AS p50_input,
       PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY input_tokens) AS p90_input,
       PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY input_tokens) AS p99_input,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (system_static_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_sys_static,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (system_dynamic_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_sys_dynamic,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (tools_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_tools,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (messages_user_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_user,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (tool_result_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_tool_result_subset,
       ROUND((SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens * (messages_assistant_bytes::double precision / total_bytes) ELSE 0 END)
             / NULLIF(SUM(CASE WHEN total_bytes > 0 AND input_tokens > 0 THEN input_tokens::double precision ELSE 0 END), 0))::numeric, 3) AS share_assistant
FROM base
GROUP BY 1, 2
ORDER BY input_tokens DESC;
```

---

## Interpreting results and next step

This feature records request sizes and provider usage; it does not establish a token-saving or quality improvement by itself. The test fixture in db/llm_context_test.go is synthetic. Capture several authorized real tasks before changing prompt assembly or compaction. Compare per-worker and per-model input-token percentiles, cache-read ratio, duration, and evidence quality with the same task and model configuration.

Worker cache-prefix splitting and bounded context are separate future changes. Neither is enabled by this telemetry flag. Keep retention bounded by the SQL purge policy above.
