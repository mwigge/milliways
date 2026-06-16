# Tiered Local Execution Runbook

## ROCm Failure

1. Run `llmctl runtime amd-qualification --arch-opt-in --evidence <evidence.json>`.
2. Confirm `/dev/kfd`, the render node, `rocminfo`, and `rocm-smi` are healthy.
3. Re-run the pinned `test-backend-ops` suite and one real-model smoke request.
4. Keep ROCm quarantined until runtime evidence passes. Use validated Vulkan, then CPU.

## Model Quarantine

1. Stop new routing to the profile.
2. Drain active requests and preserve the failure evidence.
3. Record the checksum, backend, hardware class, verifier failure, and reason.
4. Requalify on the same backend and hardware before removing quarantine.

## Stuck Load

1. Inspect load progress and elapsed time.
2. Cancel the serialized load when it exceeds its wall-time budget.
3. Release partial memory, mark the model failed, and wake queued requests.
4. Route queued work to another qualified model or the supervisor.

## Verification Failure

1. Reject the local result even when the model reports success.
2. Preserve the diff and command stdout, stderr, exit code, and timeout state.
3. Allow one configured local repair attempt.
4. Escalate to the supervisory agent after the repair budget is exhausted.

## Disable Local Execution

1. Set the global or session local-execution switch to disabled.
2. Cancel queued local nodes and mark running nodes draining.
3. Wait for bounded active work to finish or cancel it at timeout.
4. Confirm subsequent work remains with the active supervisory client.
