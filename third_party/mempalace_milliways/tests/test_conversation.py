import concurrent.futures
import os
import tempfile

import pytest

from mempalace_milliways.conversation import ConversationStore


@pytest.fixture
def store():
    file_handle = tempfile.NamedTemporaryFile(delete=False, suffix=".sqlite3")
    file_handle.close()
    conversation_store = ConversationStore(file_handle.name)
    yield conversation_store
    os.unlink(file_handle.name)


def test_conversation_start_get_end(store):
    result = store.conversation_start("conv-1", "block-a", "test prompt")
    assert result["conversation_id"] == "conv-1"
    assert result["block_id"] == "block-a"

    fetched = store.conversation_get("conv-1")
    assert fetched is not None
    assert fetched["prompt"] == "test prompt"

    ended = store.conversation_end("conv-1")
    assert ended["status"] == "ended"

    fetched = store.conversation_get("conv-1")
    assert fetched["status"] == "ended"


def test_conversation_list(store):
    for index in range(3):
        store.conversation_start(f"conv-{index}", f"block-{index}", f"prompt {index}")

    listed = store.conversation_list(limit=10)
    assert len(listed) == 3


def test_segment_start_end_lineage(store):
    store.conversation_start("conv-1", "block-a", "prompt")

    segment_one = store.segment_start("conv-1", "seg-1", "provider-1")
    assert segment_one["status"] == "active"

    segment_two = store.segment_start("conv-1", "seg-2", "provider-2")
    assert segment_two["status"] == "active"

    store.segment_end("seg-1", "switched")

    lineage = store.lineage("conv-1")
    assert len(lineage) == 2
    assert lineage[0]["segment_id"] == "seg-1"
    assert lineage[0]["end_reason"] == "switched"
    assert lineage[1]["segment_id"] == "seg-2"


def test_turn_append(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    store.segment_start("conv-1", "seg-1", "claude")

    result_one = store.turn_append("conv-1", "seg-1", "user", "user", "Hello")
    assert result_one["ordinal"] == 1

    result_two = store.turn_append("conv-1", "seg-1", "assistant", "claude", "Hi")
    assert result_two["ordinal"] == 2


def test_events_append_query(store):
    store.conversation_start("conv-1", "block-a", "prompt")

    store.event_append("conv-1", "block-a", "seg-1", "tool_use", "claude", "used search")
    store.event_append(
        "conv-1", "block-a", "seg-1", "tool_result", "claude", "got results"
    )
    store.event_append("conv-1", "block-a", "seg-1", "error", "claude", "oops")

    all_events = store.events_query("conv-1")
    assert len(all_events) == 3

    tool_events = store.events_query("conv-1", kind="tool_use")
    assert len(tool_events) == 1
    assert tool_events[0]["kind"] == "tool_use"


def test_checkpoint_save_resume(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    store.segment_start("conv-1", "seg-1", "claude")

    snapshot = {"turns": 5, "活跃": True}
    saved = store.checkpoint_save(
        "conv-1", "ckpt-1", "block-a", "seg-1", "claude", "mid-conversation", snapshot
    )
    assert saved["checkpoint_id"] == "ckpt-1"

    restored = store.checkpoint_resume("conv-1", "ckpt-1")
    assert restored is not None
    assert restored["snapshot"]["turns"] == 5


def test_checkpoint_latest(store):
    store.conversation_start("conv-1", "block-a", "prompt")

    store.checkpoint_save("conv-1", "ckpt-1", "block-a", "seg-1", "claude", "first", {"n": 1})
    store.checkpoint_save("conv-1", "ckpt-2", "block-a", "seg-1", "claude", "second", {"n": 2})

    latest = store.checkpoint_latest("conv-1")
    assert latest["checkpoint_id"] == "ckpt-2"
    assert latest["snapshot"]["n"] == 2


def test_concurrent_writes(store):
    store.conversation_start("conv-1", "block-a", "prompt")

    def append_turn(index: int) -> bool:
        store.turn_append("conv-1", "seg-1", "user", "user", f"msg-{index}")
        return True

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        futures = [executor.submit(append_turn, index) for index in range(20)]
        results = [future.result() for future in concurrent.futures.as_completed(futures)]

    assert all(results)
