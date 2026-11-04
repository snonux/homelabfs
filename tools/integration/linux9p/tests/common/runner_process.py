# SPDX-License-Identifier: GPL-2.0-only
"""Bounded cleanup for a runner's start_new_session process group."""
import os
import signal
import time


def process_running(process):
    """Observe exit without reaping the leader or releasing its PID."""
    return os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is None


def finish_process_group(process, grace=5):
    """Stop all owned group members before reaping their session leader.

    WNOWAIT keeps the leader's PID reserved even when it exits on TERM before
    its descendants. The final group KILL therefore cannot target a reused PID.
    Callers must use process_running rather than Popen.poll before this call.
    """
    try:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        deadline = time.monotonic() + grace
        while process_running(process) and time.monotonic() < deadline:
            time.sleep(.01)
    finally:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=5)
    return process.returncode
