"""Conversation primitives for milliways.

SQLite-backed storage for live conversations, segments, turns,
runtime events, and checkpoints.
"""

from __future__ import annotations

import json
import sqlite3
import threading
from datetime import datetime
from pathlib import Path

_CONVERSATION_SCHEMA = """
CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    block_id TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    ended_at TEXT
);

CREATE TABLE IF NOT EXISTS segments (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    started_at TEXT NOT NULL,
    ended_at TEXT,
    end_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS turns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    segment_id TEXT NOT NULL,
    role TEXT NOT NULL,
    provider TEXT NOT NULL,
    text TEXT NOT NULL,
    at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runtime_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    block_id TEXT NOT NULL,
    segment_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    provider TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    fields_json TEXT NOT NULL DEFAULT '{}',
    at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    checkpoint_id TEXT NOT NULL UNIQUE,
    block_id TEXT NOT NULL,
    segment_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    reason TEXT NOT NULL,
    taken_at TEXT NOT NULL,
    snapshot_json TEXT NOT NULL
);
"""


class ConversationStore:
    """Thread-safe SQLite-backed conversation store."""

    def __init__(self, db_path: str) -> None:
        self._db = db_path
        self._lock = threading.Lock()
        self._init_schema()

    def _connect(self) -> sqlite3.Connection:
        return sqlite3.connect(self._db)

    def _init_schema(self) -> None:
        Path(self._db).parent.mkdir(parents=True, exist_ok=True)
        with self._lock:
            connection = self._connect()
            connection.executescript(_CONVERSATION_SCHEMA)
            connection.close()

    def conversation_start(
        self, conversation_id: str, block_id: str, prompt: str
    ) -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "INSERT OR IGNORE INTO conversations (id, block_id, prompt, status, created_at) "
                "VALUES (?, ?, ?, 'active', ?)",
                (conversation_id, block_id, prompt, now),
            )
            connection.commit()
            row = connection.execute(
                "SELECT id, block_id, status, created_at FROM conversations WHERE id = ?",
                (conversation_id,),
            ).fetchone()
            connection.close()
        return {
            "conversation_id": row[0],
            "block_id": row[1],
            "status": row[2],
            "created_at": row[3],
        }

    def conversation_end(self, conversation_id: str) -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "UPDATE conversations SET status='ended', ended_at=? WHERE id=?",
                (now, conversation_id),
            )
            connection.commit()
            connection.close()
        return {"conversation_id": conversation_id, "status": "ended"}

    def conversation_get(self, conversation_id: str) -> dict[str, str] | None:
        with self._lock:
            connection = self._connect()
            row = connection.execute(
                "SELECT id, block_id, prompt, status, created_at, ended_at "
                "FROM conversations WHERE id=?",
                (conversation_id,),
            ).fetchone()
            connection.close()
        if not row:
            return None
        return {
            "conversation_id": row[0],
            "block_id": row[1],
            "prompt": row[2],
            "status": row[3],
            "created_at": row[4],
            "ended_at": row[5] or "",
        }

    def conversation_list(self, limit: int = 20) -> list[dict[str, str]]:
        with self._lock:
            connection = self._connect()
            rows = connection.execute(
                "SELECT id, block_id, status, created_at FROM conversations "
                "ORDER BY created_at DESC LIMIT ?",
                (limit,),
            ).fetchall()
            connection.close()
        return [
            {
                "conversation_id": row[0],
                "block_id": row[1],
                "status": row[2],
                "created_at": row[3],
            }
            for row in rows
        ]

    def turn_append(
        self,
        conversation_id: str,
        segment_id: str,
        role: str,
        provider: str,
        text: str,
    ) -> dict[str, int | str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            cursor = connection.execute(
                "SELECT COALESCE(MAX(id), 0) + 1 FROM turns WHERE conversation_id=?",
                (conversation_id,),
            )
            ordinal = cursor.fetchone()[0]
            connection.execute(
                "INSERT INTO turns (conversation_id, segment_id, role, provider, text, at) "
                "VALUES (?, ?, ?, ?, ?, ?)",
                (conversation_id, segment_id, role, provider, text, now),
            )
            connection.commit()
            connection.close()
        return {
            "conversation_id": conversation_id,
            "segment_id": segment_id,
            "ordinal": ordinal,
            "at": now,
        }

    def segment_start(
        self, conversation_id: str, segment_id: str, provider: str
    ) -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "INSERT OR IGNORE INTO segments "
                "(id, conversation_id, provider, status, started_at) VALUES (?, ?, ?, 'active', ?)",
                (segment_id, conversation_id, provider, now),
            )
            connection.commit()
            connection.close()
        return {
            "segment_id": segment_id,
            "conversation_id": conversation_id,
            "provider": provider,
            "status": "active",
            "started_at": now,
        }

    def segment_end(self, segment_id: str, reason: str = "") -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "UPDATE segments SET status='done', ended_at=?, end_reason=? WHERE id=?",
                (now, reason, segment_id),
            )
            connection.commit()
            connection.close()
        return {
            "segment_id": segment_id,
            "status": "done",
            "ended_at": now,
            "end_reason": reason,
        }

    def lineage(self, conversation_id: str) -> list[dict[str, str]]:
        with self._lock:
            connection = self._connect()
            rows = connection.execute(
                "SELECT id, provider, status, started_at, ended_at, end_reason "
                "FROM segments WHERE conversation_id=? ORDER BY started_at",
                (conversation_id,),
            ).fetchall()
            connection.close()
        return [
            {
                "segment_id": row[0],
                "provider": row[1],
                "status": row[2],
                "started_at": row[3],
                "ended_at": row[4] or "",
                "end_reason": row[5],
            }
            for row in rows
        ]

    def event_append(
        self,
        conversation_id: str,
        block_id: str,
        segment_id: str,
        kind: str,
        provider: str,
        text: str = "",
        fields: dict[str, object] | None = None,
    ) -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "INSERT INTO runtime_events "
                "(conversation_id, block_id, segment_id, kind, provider, text, fields_json, at) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    conversation_id,
                    block_id,
                    segment_id,
                    kind,
                    provider,
                    text,
                    json.dumps(fields or {}),
                    now,
                ),
            )
            connection.commit()
            connection.close()
        return {"conversation_id": conversation_id, "kind": kind, "at": now}

    def events_query(
        self,
        conversation_id: str,
        kind: str | None = None,
        since: str | None = None,
        limit: int = 100,
    ) -> list[dict[str, object]]:
        with self._lock:
            connection = self._connect()
            query = (
                "SELECT kind, provider, text, fields_json, at "
                "FROM runtime_events WHERE conversation_id=?"
            )
            arguments: list[str | int] = [conversation_id]
            if kind:
                query += " AND kind=?"
                arguments.append(kind)
            if since:
                query += " AND at>?"
                arguments.append(since)
            query += " ORDER BY at ASC LIMIT ?"
            arguments.append(limit)
            rows = connection.execute(query, arguments).fetchall()
            connection.close()
        return [
            {
                "kind": row[0],
                "provider": row[1],
                "text": row[2],
                "fields": json.loads(row[3]),
                "at": row[4],
            }
            for row in rows
        ]

    def checkpoint_save(
        self,
        conversation_id: str,
        checkpoint_id: str,
        block_id: str,
        segment_id: str,
        provider: str,
        reason: str,
        snapshot: dict[str, object],
    ) -> dict[str, str]:
        with self._lock:
            connection = self._connect()
            now = datetime.now().isoformat()
            connection.execute(
                "INSERT OR REPLACE INTO checkpoints "
                "(conversation_id, checkpoint_id, block_id, segment_id, provider, reason, taken_at, snapshot_json) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    conversation_id,
                    checkpoint_id,
                    block_id,
                    segment_id,
                    provider,
                    reason,
                    now,
                    json.dumps(snapshot),
                ),
            )
            connection.commit()
            connection.close()
        return {
            "conversation_id": conversation_id,
            "checkpoint_id": checkpoint_id,
            "taken_at": now,
        }

    def checkpoint_resume(
        self, conversation_id: str, checkpoint_id: str
    ) -> dict[str, object] | None:
        with self._lock:
            connection = self._connect()
            row = connection.execute(
                "SELECT checkpoint_id, block_id, segment_id, provider, reason, taken_at, snapshot_json "
                "FROM checkpoints WHERE conversation_id=? AND checkpoint_id=?",
                (conversation_id, checkpoint_id),
            ).fetchone()
            connection.close()
        if not row:
            return None
        return {
            "checkpoint_id": row[0],
            "block_id": row[1],
            "segment_id": row[2],
            "provider": row[3],
            "reason": row[4],
            "taken_at": row[5],
            "snapshot": json.loads(row[6]),
        }

    def checkpoint_latest(self, conversation_id: str) -> dict[str, object] | None:
        with self._lock:
            connection = self._connect()
            row = connection.execute(
                "SELECT checkpoint_id, block_id, segment_id, provider, reason, taken_at, snapshot_json "
                "FROM checkpoints WHERE conversation_id=? ORDER BY taken_at DESC LIMIT 1",
                (conversation_id,),
            ).fetchone()
            connection.close()
        if not row:
            return None
        return {
            "checkpoint_id": row[0],
            "block_id": row[1],
            "segment_id": row[2],
            "provider": row[3],
            "reason": row[4],
            "taken_at": row[5],
            "snapshot": json.loads(row[6]),
        }
