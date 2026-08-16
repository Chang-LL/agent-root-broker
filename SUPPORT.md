# Support

hostctl is alpha software. Use GitHub Issues for reproducible bugs, installation failures, and
focused questions. Feature proposals should describe the intended trust model, not only the user
interface.

Before opening an issue, check [COMPATIBILITY.md](COMPATIBILITY.md) and
[TROUBLESHOOTING.md](TROUBLESHOOTING.md). Include:

- `hostctl version` and the release archive architecture;
- distribution, release, kernel architecture, and systemd version;
- agent/Grok version and whether lifecycle hooks load;
- filesystem type when the problem involves home access;
- the smallest reproducible command using harmless operands; and
- relevant `systemctl status hostctld` or `journalctl -u hostctld` output after redaction.

Never include access tokens, cookies, proxy credentials, private keys, real hostnames/IP addresses,
unredacted home paths, or sensitive command arguments. `hostctl --json doctor` is safe to start
with, but review all diagnostics before posting them.

Security issues belong in a private vulnerability report; see [SECURITY.md](SECURITY.md).
