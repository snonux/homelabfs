#!/usr/bin/env python3
"""Sequential owned exporter tests; invoke under the task's resource guard."""
import os
from pathlib import Path
import platform
import subprocess
import sys

source = Path(__file__).resolve().parents[1]
output = Path(sys.argv[1]).resolve()
output.mkdir(mode=0o700)
for generator, name in [("fixtures.py", "fixtures.h"),
                        ("terminal_fixtures.py", "terminal_fixtures.h")]:
    subprocess.run([sys.executable, str(source / "tests" / generator),
                    str(output / name)], check=True)
flags = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-O1", "-g",
         "-fsanitize=address,undefined"]
if platform.system() == "Linux":
    flags += ["-D_DEFAULT_SOURCE", "-D_POSIX_C_SOURCE=200809L"]
crypto = "-lmd" if platform.system() == "FreeBSD" else "-lcrypto"
subprocess.run([os.environ.get("CC", "cc"), *flags, "-I" + str(output),
                str(source / "tests/terminal_test.c"),
                str(source / "src/hfs9p_json.c"),
                str(source / "src/hfs9p_ledger.c"), "-pthread", crypto,
                "-o", str(output / "terminal_test")], check=True)
subprocess.run([str(output / "terminal_test"), str(output / "ledger"), *sys.argv[2:]], check=True)
