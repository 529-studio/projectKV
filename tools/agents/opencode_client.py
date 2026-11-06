#!/usr/bin/env python3
"""
OpenCode Sub-Agent Client & Quota-Failover Relay.

Orchestrates DeepSeek V4.1 Flash Free from OpenCode on this device,
with automatic failover to opencode-go/deepseek-v4.1-flash when quota/rate limits are hit.
Provides specialized SDLC personas: coder, reviewer, reporter, po.
"""

import argparse
import os
import re
import subprocess
import sys

PRIMARY_MODEL = "opencode/deepseek-v4.1-flash-free"
FALLBACK_MODEL = "opencode-go/deepseek-v4.1-flash"

QUOTA_ERROR_PATTERNS = [
    r"quota",
    r"rate[\s_-]?limit",
    r"429",
    r"exhausted",
    r"insufficient",
    r"credit",
    r"too many requests",
    r"capacity",
    r"overloaded",
]

ROLE_PROMPTS = {
    "coder": (
        "You are the Principal Software Engineer on ProjectKV (real-time urban monitoring). "
        "Strict rules: "
        "1. Write clean, sound, production-ready code addressing problems at root cause (Rule 2). "
        "2. Zero emojis in code, logs, and comments (Rule 1). "
        "3. Follow Go leaf modulith architecture (internal packages never cross-import each other). "
        "4. Hand-written raw SQL only (no ORM). "
        "5. No hardcoded secrets or non-empty fallback credentials (Rule 8). "
        "Do not invoke shell tools or git commands. Provide direct, complete, self-contained implementation in markdown."
    ),
    "reviewer": (
        "You are the Senior Staff Code Reviewer and Architecture Guardian on ProjectKV. "
        "Rigorously audit the provided diff or code against: "
        "1. Correctness and edge-case handling. "
        "2. Leaf modulith compliance (no sibling internal/ imports in go-server). "
        "3. Concurrency safety, race conditions, and deadlocks. "
        "4. Zero emojis in logs, strings, and UI (Rule 1). "
        "5. Zero hardcoded secrets (Rule 8). "
        "6. Test quality and assertions. "
        "Do not invoke shell tools or git commands. Output structured feedback: Findings (Blocker, Major, Nit) and Final Verdict (APPROVED / REVISE)."
    ),
    "reporter": (
        "You are the Technical Auditor and Sprint Reporter for ProjectKV. "
        "Generate a factual, objective, data-backed completion report following the exact standard "
        "of docs/reports/sprint8_report.md. "
        "Strict rules: zero emojis, ground truth verified against git PR ledger, quantitative scorecard, "
        "reality-check retrospective, and new lessons learned. Do not invoke shell tools or git commands."
    ),
    "po": (
        "You are the Technical Product Owner for ProjectKV. "
        "You evaluate sprint deliverables against ADR goals and Definition of Done. "
        "You design realistic, high-impact architecture epics, wave dependency maps, and "
        "formal GitHub issue specifications with Rule 3 estimates. Do not invoke shell tools or git commands."
    ),
}


def is_quota_error(text: str) -> bool:
    for pattern in QUOTA_ERROR_PATTERNS:
        if re.search(pattern, text, re.IGNORECASE):
            return True
    return False


def run_opencode(model: str, full_prompt: str, timeout: int = 180) -> tuple[int, str, str]:
    cmd = ["opencode", "run", "-m", model, full_prompt]
    try:
        proc = subprocess.run(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=timeout,
        )
        return proc.returncode, proc.stdout, proc.stderr
    except subprocess.TimeoutExpired:
        return 124, "", f"Execution timed out ({timeout}s)"


def query_agent(
    prompt: str,
    role: str = "coder",
    model_override: str | None = None,
) -> str:
    system_prompt = ROLE_PROMPTS.get(role, ROLE_PROMPTS["coder"])
    composed_prompt = f"[SYSTEM PERSONA: {role.upper()}]\n{system_prompt}\n\n[USER TASK]\n{prompt}"

    primary = model_override or PRIMARY_MODEL
    sys.stderr.write(f"[opencode_client] Invoking agent ({role}) via {primary}...\n")
    code, stdout, stderr = run_opencode(primary, composed_prompt)

    combined_err = f"{stderr}\n{stdout}"
    if code != 0 or is_quota_error(combined_err):
        if not model_override and primary != FALLBACK_MODEL:
            sys.stderr.write(
                f"[opencode_client] Primary model {primary} failed/hit quota limit (code={code}). "
                f"Switching failover to {FALLBACK_MODEL}...\n"
            )
            code_fb, stdout_fb, stderr_fb = run_opencode(FALLBACK_MODEL, composed_prompt)
            if code_fb != 0:
                sys.stderr.write(f"[opencode_client] Fallback error (code={code_fb}): {stderr_fb}\n")
                if not stdout_fb.strip():
                    raise RuntimeError(f"OpenCode fallback failed: {stderr_fb.strip()}")
            return stdout_fb
        else:
            if not stdout.strip():
                raise RuntimeError(f"OpenCode execution failed (code={code}): {stderr.strip()}")

    return stdout


def main():
    parser = argparse.ArgumentParser(description="OpenCode Multi-Agent Relay Client")
    parser.add_argument("prompt", nargs="?", default="", help="Prompt text")
    parser.add_argument("--role", choices=["coder", "reviewer", "reporter", "po"], default="coder")
    parser.add_argument("--file", "-f", help="Read prompt from file")
    parser.add_argument("--stdin", action="store_true", help="Read prompt from stdin")
    parser.add_argument("--diff", action="store_true", help="Append current git diff to prompt")
    parser.add_argument("--model", "-m", help="Explicit model override")

    args = parser.parse_args()

    content = ""
    if args.stdin:
        content = sys.stdin.read()
    elif args.file:
        with open(args.file, "r", encoding="utf-8") as f:
            content = f.read()
    elif args.prompt:
        content = args.prompt
    else:
        parser.error("No prompt provided. Pass a prompt, --file, or --stdin.")

    if args.diff:
        diff_proc = subprocess.run(["git", "diff"], stdout=subprocess.PIPE, text=True)
        content += f"\n\n[GIT DIFF]\n```diff\n{diff_proc.stdout}\n```"

    try:
        response = query_agent(content, role=args.role, model_override=args.model)
        print(response)
    except Exception as e:
        sys.stderr.write(f"[ERROR] {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
