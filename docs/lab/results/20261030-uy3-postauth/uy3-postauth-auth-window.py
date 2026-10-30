"""ROOT-reviewed read-only diagnostic proposal; do not execute automatically."""
import datetime
import json
import re
from pathlib import Path

LIMIT = 1 << 20
SOURCE = "192.168.1.246"
WINDOW = ("19:49:45", "19:50:15")  # 2026-10-30, host UTC+02:00

path = Path("/var/log/auth.log")
with path.open("rb") as stream:
    stream.seek(0, 2)
    size = stream.tell()
    start = max(0, size - LIMIT)
    stream.seek(start)
    data = stream.read(LIMIT)
if start:
    data = data.partition(b"\n")[2]

events = []
observed_window_rows = 0
for raw in data.splitlines():
    line = raw.decode("utf-8", errors="replace")
    stamp = re.match(r"^Oct\s+30 (\d\d:\d\d:\d\d) ", line)
    if not stamp or not WINDOW[0] <= stamp[1] <= WINDOW[1]:
        continue
    observed_window_rows += 1
    if not re.search(r"(?<![0-9.])" + re.escape(SOURCE) + r"(?![0-9.])", line):
        continue
    proc = re.search(r"\b(sshd(?:-session)?)\[(\d+)\]: (.*)$", line)
    if not proc:
        continue
    message = proc[3]
    kind = "other controller SSH event (content omitted)"
    for prefix, label in (
        ("Accepted publickey for paul ", "publickey accepted"),
        ("Disconnected from user paul ", "authenticated disconnect"),
        ("Received disconnect from ", "disconnect received"),
        ("Connection closed by ", "connection closed"),
        ("Timeout before authentication", "preauthentication timeout"),
        ("Timeout, client not responding", "server client-liveness timeout"),
    ):
        if message.startswith(prefix):
            kind = label
            break
    port = re.search(r"\bport (\d{1,5})\b", message)
    events.append({
        "local_time": stamp[1], "process": proc[1], "pid": int(proc[2]),
        "kind": kind, "client_port": int(port[1]) if port else None,
        "preauth_marker": "[preauth]" in message,
    })
    if len(events) >= 32:
        break

result = {
    "observed_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "host_offset": datetime.datetime.now().astimezone().strftime("%z"),
    "path": str(path), "tail_bytes_read": len(data), "source": SOURCE,
    "requested_host_local_window": ["2026-10-30 " + v for v in WINDOW],
    "other_window_rows_count": observed_window_rows - len(events),
    "events": events,
    "limit": "Only current auth.log tail up to1MiB; absent rows do not prove absent authentication or dispatch.",
}
print(json.dumps(result, indent=2))
