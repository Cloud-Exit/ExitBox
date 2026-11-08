#!/usr/bin/env python3
"""exitbox-allow-ipc.py — domain allow request via IPC socket.

Standalone Python IPC client for requesting domain access from the ExitBox
host. This script exists as a fallback for environments (e.g. Codex) where
the Go binary and shell wrapper are blocked by seccomp sandbox restrictions.

Invoke directly as:  python3 /usr/local/bin/exitbox-allow-ipc.py <domain> ...
"""

import json
import os
import secrets
import socket
import sys


def request_allow(sock_path, domain):
    """Send an allow_domain IPC request and return (approved, firewall, error)."""
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(sock_path)
    except OSError as exc:
        return False, True, (
            f"IPC socket not available ({exc}). "
            "Domain allow requests need the host exitbox process to be running"
        )

    req = json.dumps({
        "type": "allow_domain",
        "id": secrets.token_hex(8),
        "payload": {"domain": domain},
    }) + "\n"
    sock.sendall(req.encode())

    buf = b""
    while b"\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            break
        buf += chunk
    sock.close()

    if not buf.strip():
        return False, True, "no response from host"

    resp = json.loads(buf)
    payload = resp.get("payload", {})
    if isinstance(payload, str):
        payload = json.loads(payload)

    err = payload.get("error", "")
    if err:
        return False, True, err

    # Absent field means an older host: assume the firewall is on.
    firewall = payload.get("firewall_enabled", True)

    return payload.get("approved", False), firewall, None


def main():
    if len(sys.argv) < 2:
        print("Usage: python3 exitbox-allow-ipc.py <domain> [domain ...]", file=sys.stderr)
        sys.exit(1)

    sock_path = os.environ.get("EXITBOX_IPC_SOCKET", "/run/exitbox/host.sock")
    failed = False

    for domain in sys.argv[1:]:
        approved, firewall, err = request_allow(sock_path, domain)
        if err:
            print(f"Error: {domain}: {err}", file=sys.stderr)
            failed = True
            continue
        if not firewall:
            print(f"Approved: {domain} (no firewall, all domains reachable)")
        elif approved:
            print(f"Approved: {domain}")
        else:
            print(f"Denied: {domain}")
            failed = True

    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
