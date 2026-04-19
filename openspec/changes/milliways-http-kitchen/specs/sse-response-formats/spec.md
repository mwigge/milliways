## ADDED Requirements

### Requirement: OpenAI SSE format (default)

OpenAI-compatible endpoints (OpenAI, Groq, Deepseek, Perplexity, Mistral, OpenRouter) use standard OpenAI SSE:

**Request body:**
```json
POST /v1/chat/completions
{
  "model": "<model>",
  "stream": true,
  "messages": [{"role": "user", "content": "<prompt>"}]
}
```

**SSE chunks:**
```
data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: [DONE]
```

**Parser SHALL:**
1. Skip lines not starting with `data: `
2. Skip `data: [DONE]`
3. Unmarshal JSON: `{"choices":[{"delta":{"content":"..."}}]}`
4. Extract `choices[0].delta.content`
5. Signal `done=true` when `choices[0].finish_reason == "stop"` AND chunk is not `chat.completion.chunk`

#### Scenario: OpenAI SSE chunk extraction
- **WHEN** line is `data: {"choices":[{"delta":{"content":"Hello"}}]}`
- **THEN** parser SHALL return `content = "Hello"`, `done = false`
- **WHEN** line is `data: [DONE]`
- **THEN** parser SHALL return `done = true`, `content = ""`

---

### Requirement: Anthropic SSE format

Claude (api.anthropic.com) uses a different endpoint and event format:

**Request body:**
```json
POST /v1/messages
anthropic-version: 2023-06-01
{
  "model": "<model>",
  "stream": true,
  "max_tokens": 8192,
  "messages": [{"role": "user", "content": [{"type": "text", "text": "<prompt>"}]}]
}
```

**SSE events:**
```
event: anthropic
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
event: anthropic
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}
event: anthropic
data: {"type":"message_stop"}
```

**Parser SHALL:**
1. Skip lines not starting with `data: `
2. Extract `type` field from JSON — only process `content_block_delta`
3. Extract `delta.text` field
4. Signal `done=true` when `type == "message_stop"`

#### Scenario: Anthropic SSE event extraction
- **WHEN** line is `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
- **THEN** parser SHALL return `content = "Hello"`, `done = false`
- **WHEN** line is `data: {"type":"message_stop"}`
- **THEN** parser SHALL return `done = true`, `content = ""`

---

### Requirement: MiniMax SSE format

MiniMax (api.minimaxi.com) uses OpenAI-compatible SSE with custom endpoint:

**Request body:**
```json
POST /v1/text/chatcompletion_v2
Authorization: Bearer <key>
{
  "model": "M2-her",
  "stream": true,
  "messages": [{"role": "user", "content": "<prompt>"}]
}
```

**SSE chunks:**
```
data: {"choices":[{"delta":{"content":"Hello"}}...]}
data: {"choices":[{"delta":{"content":" world"}}...]}
```

**Parser:** Same as OpenAI format — `choices[0].delta.content`, `done` on finish_reason=stop.

#### Scenario: MiniMax SSE chunk extraction
- **WHEN** line is `data: {"choices":[{"delta":{"content":"你好"}}]}`
- **THEN** parser SHALL return `content = "你好"`, `done = false`
- **AND** SHALL use `minimax` parser variant

---

### Requirement: Ollama SSE format

Ollama (localhost:11434) uses a different format:

**Request body:**
```json
POST /api/chat
{
  "model": "<model>",
  "stream": true,
  "messages": [{"role": "user", "content": "<prompt>"}]
}
```

**SSE chunks:**
```
data: {"message":{"content":"Hello"},"done":false}
data: {"message":{"content":" world"},"done":true}
```

**Parser SHALL:**
1. Skip lines not starting with `data: `
2. Unmarshal JSON: `{"message":{"content":"..."},"done":true|false}`
3. Extract `message.content`
4. Signal `done=true` when `done == true`

#### Scenario: Ollama SSE chunk extraction
- **WHEN** line is `data: {"message":{"content":"Hello"},"done":false}`
- **THEN** parser SHALL return `content = "Hello"`, `done = false`
- **WHEN** line is `data: {"message":{"content":""},"done":true}`
- **THEN** parser SHALL return `done = true`, `content = ""`

---

### Requirement: Non-200 error handling

**Parser SHALL:**
1. Read response body (up to 1024 bytes) on non-200 status
2. Return error containing status code and body excerpt
3. NOT attempt SSE parsing on error responses

#### Scenario: Non-200 response
- **WHEN** API returns HTTP 401
- **THEN** result error SHALL contain `"401"`
- **AND** SHALL contain first 1024 bytes of body

### Requirement: Context cancellation

**Parser SHALL:**
1. Check `ctx.Done()` channel in SSE read loop
2. Return partial output collected so far if context cancelled
3. Set non-zero exit code on cancellation

#### Scenario: Context cancelled mid-stream
- **WHEN** context is cancelled after 3 chunks received
- **THEN** result SHALL contain accumulated content from those 3 chunks
- **AND** exit code SHALL be non-zero
