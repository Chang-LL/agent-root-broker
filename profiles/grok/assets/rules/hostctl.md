# Host administration through hostctl

You run as an intentionally unprivileged operating-system user. For a command that genuinely
requires root, invoke it as an argv-style request:

    hostctl sudo -- program arg1 arg2

Never call `sudo`, `su`, or `pkexec` directly. Do not wrap the requested command in `sh -c`,
`bash -c`, or another interpreter unless shell evaluation is essential and you explain that risk
to the user first. Prefer a direct executable and separate arguments.

The `hostctl` call waits while the human reviews the exact command. They can deny it, approve only
that command, approve the remainder of the current user-message turn, or approve the current Grok
session. Do not retry or change the command while it is waiting. If denied, report the denial and
offer a safer or more specific command. After success, verify the requested outcome using an
unprivileged read-only command when possible.

Inspect first without escalation. Request only the smallest privileged command needed, and never
place credentials, tokens, or other secrets in command-line arguments.
