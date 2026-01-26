# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
# See the LICENSE.txt file for the full license text.
#
# NOTE: This work is subject to additional terms under AGPL v3 Section 7.
# See the NOTICE.txt file for details regarding AI system attribution.

# Gemini-powered code review for GitHub Actions

import os
import sys
from typing import Optional

from google import genai
from github import Github, Auth
from github.PullRequest import PullRequest
from github.File import File

# --- Configuration ---
MAX_DIFF_CHARS = 30_000  # Gemini context limit safety
REQUEST_TIMEOUT = 60  # seconds


def get_env_or_exit(name: str) -> str:
    """Get required environment variable or exit with error."""
    value = os.getenv(name)
    if not value:
        print(f"❌ Missing required environment variable: {name}")
        sys.exit(1)
    return value


# 1. Setup - with validation
GITHUB_TOKEN = get_env_or_exit("GITHUB_TOKEN")
GEMINI_API_KEY = get_env_or_exit("GEMINI_API_KEY")
REPO_NAME = get_env_or_exit("REPO_NAME")

try:
    PR_NUMBER = int(os.getenv("PR_NUMBER", ""))
except ValueError:
    print("❌ PR_NUMBER must be a valid integer")
    sys.exit(1)

# 2. Configure Gemini
client = genai.Client(api_key=GEMINI_API_KEY)


def get_pr_diff() -> tuple[list[File], PullRequest]:
    """Fetch PR files and metadata from GitHub."""
    g = Github(auth=Auth.Token(GITHUB_TOKEN), timeout=REQUEST_TIMEOUT)
    repo = g.get_repo(REPO_NAME)
    pr = repo.get_pull(PR_NUMBER)
    return list(pr.get_files()), pr


def generate_review(diff_text: str) -> str:
    """Send diff to Gemini for code review."""
    prompt = f"""
You are a Principal Software Engineer conducting a thorough code review for a production system.
This codebase handles financial data, time series forecasting, and privacy-sensitive RAG operations.

## Review Criteria

### 1. CRITICAL: Security (Reject PR if violated)
- **Input Validation**: All external input must be validated (type, length, format, range)
- **Injection Prevention**: Check for SQL injection, command injection, XSS, path traversal
- **Secrets**: No hardcoded API keys, passwords, or tokens. Must use env vars or secret managers
- **Deserialization**: Python must use `json.loads()` only. REJECT any use of `pickle`, `yaml.load()` without SafeLoader
- **Shell Execution**: Python `subprocess` must use array form `["cmd", "arg"]`. REJECT `shell=True`
- **File Access**: Verify path sanitization. Check for `../` traversal attacks
- **Auth/AuthZ**: Verify resource ownership checks (BOLA protection)

### 2. Go-Specific Standards
- **Error Handling**: All errors must be checked. No `_` for error returns unless explicitly justified
- **Context Usage**: All I/O operations must accept `context.Context` for timeouts/cancellation
- **Concurrency**: Check for race conditions, proper mutex usage, goroutine leaks
- **Resource Cleanup**: Verify `defer` for Close(), proper resource lifecycle
- **Nil Checks**: Guard against nil pointer dereference, especially for interface returns
- **Logging**: Use structured logging (slog). Include trace_id for observability
- **Naming**: Follow Go conventions (MixedCaps, not snake_case)

### 3. Python-Specific Standards
- **Type Hints**: Functions should have type annotations
- **Exception Handling**: Catch specific exceptions, not bare `except:`
- **Resource Management**: Use context managers (`with`) for files, connections
- **Pydantic/FastAPI**: Validate request models, use proper status codes

### 4. Architecture & Design
- **Single Responsibility**: Each function/method should do one thing well
- **Separation of Concerns**: Business logic separate from HTTP handlers
- **No God Objects**: Classes/structs should not exceed ~300 lines

### 5. Performance & Reliability
- **Unbounded Operations**: Flag unbounded loops, recursion without limits, unlimited retries
- **Timeouts**: All external calls must have timeouts
- **Retries**: Must have exponential backoff and max attempts

## Response Format

**🔴 CRITICAL** (Must fix before merge)
- [File:Line] Issue and fix

**🟡 IMPORTANT** (Should fix)
- [File:Line] Issue and fix

**🟢 SUGGESTIONS** (Nice to have)
- [File:Line] Suggestion

If clean, respond: "✅ LGTM - [brief summary]"

Do not comment on formatting, import order, or line length.

---

## Diff to Review:

```diff
{diff_text}
```
"""
    try:
        response = client.models.generate_content(
            model="gemini-2.5-pro",
            contents=prompt,
        )
        return response.text
    except Exception as e:
        return f"❌ Gemini API error: {e}"


def main() -> None:
    print("📥 Fetching PR diff from GitHub...")
    try:
        files, pr = get_pr_diff()
    except Exception as e:
        print(f"❌ Failed to fetch PR: {e}")
        sys.exit(1)

    # Build diff, respecting size limit
    full_diff = ""
    truncated = False
    for file in files:
        if file.patch:
            addition = f"File: {file.filename}\n{file.patch}\n\n"
            if len(full_diff) + len(addition) > MAX_DIFF_CHARS:
                truncated = True
                break
            full_diff += addition

    if not full_diff:
        print("ℹ️ No code changes found to review.")
        return

    if truncated:
        print(f"⚠️ Diff truncated to {MAX_DIFF_CHARS} chars")

    print("🤖 Sending diff to Gemini...")
    review = generate_review(full_diff)

    print("💬 Posting comment to GitHub...")
    try:
        header = "## 🤖 Gemini Code Review\n\n"
        if truncated:
            header += "> ⚠️ Large PR - only first ~30k chars reviewed\n\n"
        pr.create_issue_comment(header + review)
        print("✅ Review posted successfully")
    except Exception as e:
        print(f"❌ Failed to post comment: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
