# Spec: exhaustion-detection

## Overview

Milliways MUST detect provider exhaustion from both structured adapter events and plain-text CLI output. Exhaustion detection drives continuity failover and quota updates.

## Requirements

### Detection sources

- Adapters MUST detect exhaustion from structured protocol events when the provider exposes them
- Adapters MUST also inspect human-readable stdout and stderr for exhaustion messages
- Plain-text exhaustion detection MUST be implemented for every first-class provider adapter

### Claude-style human-readable limits

- Messages such as `You've hit your limit · resets 10pm (Europe/Stockholm)` MUST be recognized as exhaustion
- When a reset time and timezone are present, Milliways MUST parse them into a concrete absolute timestamp
- If only partial reset information is available, Milliways MUST still mark the provider exhausted and preserve the raw text

### Normalized event emission

- When exhaustion is detected, the adapter MUST emit a normalized `EventRateLimit` or equivalent exhaustion event
- The emitted exhaustion event MUST indicate:
  - provider name
  - exhaustion status
  - detection source
  - raw matched text when available
  - reset time when parseable
- Exhaustion detection SHOULD also emit a structured runtime event into the central observability stream so failover reasoning is transparent in the UI and replayable later

### Non-exhaustion safety

- Adapters MUST avoid classifying unrelated provider errors as exhaustion
- Unrecognized text MUST remain regular output or regular error events

### Integration effects

- Exhaustion detection MUST update the quota store
- Exhaustion detection MUST trigger provider continuity failover when the conversation is still active
