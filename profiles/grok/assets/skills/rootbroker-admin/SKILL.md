---
name: rootbroker-admin
description: Use when a host administration task requires root privileges, including mounting disks, managing system services, installing operating-system packages, editing protected configuration, or inspecting root-only state. Routes the exact command through rootbroker for human approval and explains command, message, and session approval behavior.
---

# Agent Root Broker Administration

Use `rootbroker` only after unprivileged inspection shows that root access is required.

## Workflow

1. Inspect the current state with read-only, unprivileged commands.
2. Explain the intended privileged change and its important side effects.
3. Submit a direct argv-style command:

   ```sh
   rootbroker sudo -- program arg1 arg2
   ```

4. Wait for the human decision. Do not retry, background, split, or mutate a pending request.
5. If approved and successful, verify the result with an unprivileged command when possible.
6. If denied, report that clearly and propose a narrower alternative instead of bypassing rootbroker.

## Approval scopes

- `command`: only the exact displayed argv, working directory, timeout, and request hash.
- `message`: subsequent rootbroker requests in this same user-prompt/assistant-turn pair, until turn end or TTL.
- `session`: subsequent rootbroker requests in this Grok process and conversation, until exit, revocation, or TTL.

The human chooses the scope. Never imply that a broader approval is required.

## Safety rules

- Never invoke `sudo`, `su`, or `pkexec` directly.
- Prefer direct executables. Avoid `sh -c`, `bash -c`, interpreters, and inline scripts because a broad
  approval could authorize content that is harder to review.
- Never put passwords, API keys, tokens, or private keys in argv.
- Keep each request minimal and understandable.
- Treat denial, expiry, daemon unavailability, and a nonzero exit code as real failures.
- Do not claim success until verification supports it.
