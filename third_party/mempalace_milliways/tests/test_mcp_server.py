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


def test_tool_conversation_start_handler(store):
    result = store.conversation_start("conv-x", "block-y", "hello")
    assert result["conversation_id"] == "conv-x"
    assert result["block_id"] == "block-y"
    assert result["status"] == "active"


def test_tool_conversation_end_handler(store):
    store.conversation_start("conv-x", "block-y", "hello")
    result = store.conversation_end("conv-x")
    assert result["status"] == "ended"


def test_tool_conversation_get_not_found(store):
    result = store.conversation_get("nonexistent")
    assert result is None


def test_tool_conversation_list_handler(store):
    for index in range(3):
        store.conversation_start(f"conv-{index}", f"block-{index}", f"p{index}")
    result = store.conversation_list(limit=10)
    assert len(result) == 3


def test_tool_turn_append(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    store.segment_start("conv-1", "seg-1", "provider-x")
    result = store.turn_append("conv-1", "seg-1", "user", "user", "hello")
    assert result["ordinal"] == 1


def test_tool_segment_lifecycle(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    started = store.segment_start("conv-1", "seg-1", "claude")
    assert started["status"] == "active"

    ended = store.segment_end("seg-1", "done")
    assert ended["status"] == "done"


def test_tool_checkpoint_resume(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    store.segment_start("conv-1", "seg-1", "claude")
    store.checkpoint_save("conv-1", "ckpt-1", "block-a", "seg-1", "claude", "test", {"k": "v"})

    restored = store.checkpoint_resume("conv-1", "ckpt-1")
    assert restored["snapshot"]["k"] == "v"
    assert restored["checkpoint_id"] == "ckpt-1"


def test_tool_events_query(store):
    store.conversation_start("conv-1", "block-a", "prompt")
    store.event_append("conv-1", "block-a", "seg-1", "user_turn", "user", "hello")
    store.event_append("conv-1", "block-a", "seg-1", "assistant_turn", "claude", "hi")

    events = store.events_query("conv-1")
    assert len(events) == 2
