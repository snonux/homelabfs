#!/usr/bin/env python3
"""Require an actual complete real-command root test, with no fail/skip events."""

import json
from pathlib import Path
import sys


PACKAGE = "github.com/snonux/homelabfs/internal/daemoncli"
TEST = "TestLinuxBehaviorCLIIntegration"


def validate(path):
    events = [json.loads(line) for line in Path(path).read_text().splitlines()]
    if not events:
        raise ValueError("missing command test events")
    runs, passes, packages = [], [], []
    for index, event in enumerate(events):
        action = event.get("Action")
        if action in {"fail", "skip", "build-fail"}:
            raise ValueError(f"command test has forbidden {action} event: {event}")
        # go test may report compiler build-output events before package start.
        # They cannot satisfy any test/package success requirement.
        if action in {"run", "pass"}:
            if event.get("Package") != PACKAGE:
                raise ValueError(f"unexpected test package: {event}")
            test = event.get("Test")
            if test:
                if test != TEST:
                    raise ValueError(f"unexpected command test: {test}")
                if action == "run":
                    runs.append(index)
                else:
                    passes.append(index)
            elif action == "pass":
                packages.append(index)
    if len(runs) != 1 or len(passes) != 1 or len(packages) != 1:
        raise ValueError("require exact parent test run/pass and package pass")
    if not runs[0] < passes[0] < packages[0]:
        raise ValueError("command test success events are incomplete or unordered")


def main():
    if len(sys.argv) != 2:
        raise ValueError("usage: validate-linux-behavior-cli.py GO_JSON_EVENTS")
    validate(sys.argv[1])
    print("real behavior command: root test and package passed without fail/skip")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError) as error:
        print(f"behavior command evidence rejected: {error}", file=sys.stderr)
        sys.exit(1)
