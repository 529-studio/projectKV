#!/usr/bin/env python3
"""Sprint-boundary reminders for agy lifecycle hooks.

Wired from `.agents/hooks.json`; the rule it enforces is AGENTS.md #12. The hook
must never break a session, so every failure path exits 0 with an empty payload.

    echo '{"conversationId":"...","cwd":"..."}' | python3 scripts/sprint_boundary.py pre-invocation
"""

from __future__ import annotations

import json
import re
import sqlite3
import sys
from pathlib import Path

AGENTS_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = AGENTS_DIR.parent
STATE_FILE = AGENTS_DIR / ".state" / "sprint-boundary.json"

# Steps in the conversation DB, then how often the reminder may repeat.
WARN_STEPS = 1_200
HARD_STEPS = 2_500
REPEAT_EVERY = 500

SPRINT_RE = re.compile(r"(?:^|[/_-])s(\d{1,2})(?:$|[/_.-])", re.IGNORECASE)


def emit(message: str) -> None:
    print(
        json.dumps(
            {"injectSteps": [{"systemMessage": {"systemMessage": message}}]},
            ensure_ascii=False,
        )
    )


def load_state() -> dict:
    try:
        return json.loads(STATE_FILE.read_text())
    except Exception:
        return {}


def save_state(state: dict) -> None:
    try:
        STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
        STATE_FILE.write_text(json.dumps(state))
    except Exception:
        pass


def conversation_steps(conversation_id: str) -> int | None:
    if not conversation_id:
        return None
    for product in ("antigravity-cli", "antigravity"):
        db = Path.home() / ".gemini" / product / "conversations" / f"{conversation_id}.db"
        if not db.is_file():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=0.5)
            try:
                row = con.execute("SELECT max(idx) FROM steps").fetchone()
            finally:
                con.close()
            if row and row[0] is not None:
                return int(row[0]) + 1
        except Exception:
            continue
    return None


def current_sprint() -> int | None:
    """Sprint number from the checked-out branch (`feat/s11-...`), else None."""
    try:
        head = (REPO_ROOT / ".git" / "HEAD").read_text().strip()
    except Exception:
        return None
    if not head.startswith("ref: "):
        return None
    branch = head.rsplit("/", 1)[-1]
    match = SPRINT_RE.search(branch)
    return int(match.group(1)) if match else None


def handoff_path(sprint: int | None) -> Path | None:
    if sprint is None:
        return None
    return REPO_ROOT / "docs" / f"HANDOFF-s{sprint}.md"


def on_session_start() -> None:
    sprint = current_sprint()
    where = f" Sprint {sprint} is the current span." if sprint else ""
    emit(
        "Sprint boundary protocol (AGENTS.md #12): one conversation covers one sprint."
        f"{where} If a previous `docs/HANDOFF-s*.md` exists, read it before"
        " the first task. When this sprint's tasks are merged and its report is"
        " committed, run the `sprint-close` skill and start a new conversation instead"
        " of continuing here."
    )


def on_pre_invocation(payload: dict) -> None:
    steps = conversation_steps(payload.get("conversationId", ""))
    if steps is None or steps < WARN_STEPS:
        return

    sprint = current_sprint()
    handoff = handoff_path(sprint)
    state = load_state()
    cid = payload.get("conversationId", "unknown")
    record = state.get(cid, {})
    last = int(record.get("last", 0))

    if handoff is not None and handoff.exists():
        if record.get("closed_notified"):
            return
        message = (
            f"Sprint {sprint} is already closed: `{handoff.relative_to(REPO_ROOT)}` exists."
            f" This conversation is at ~{steps} steps and is now a historical reference."
            " Do not start new work here; answer questions and point the user to the"
            " handoff and the new conversation."
        )
        record["closed_notified"] = True
    else:
        if last and steps - last < REPEAT_EVERY:
            return
        target = (
            f"`{handoff.relative_to(REPO_ROOT)}`"
            if handoff is not None
            else "`docs/HANDOFF-s<N>.md`"
        )
        urgency = "Hard boundary reached." if steps >= HARD_STEPS else "Boundary approaching."
        message = (
            f"{urgency} This conversation is at ~{steps} steps. At the end of this sprint,"
            f" run the `sprint-close` skill: write {target}, commit it, then run"
            " `tools/sprint/close.sh --launch` to start the next conversation. Do not"
            " begin the next sprint in this one."
        )

    record["last"] = steps
    state[cid] = record
    save_state(state)
    emit(message)


def main(argv: list[str]) -> int:
    mode = argv[1] if len(argv) > 1 else ""
    try:
        raw = sys.stdin.read()
        payload = json.loads(raw) if raw.strip() else {}
    except Exception:
        payload = {}

    try:
        if mode == "session-start":
            on_session_start()
        elif mode == "pre-invocation":
            on_pre_invocation(payload if isinstance(payload, dict) else {})
    except Exception:
        return 0
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
