#!/usr/bin/env python3
"""Export literal protocol inputs plus explicitly derived routing test cases."""
import copy
import hashlib
import json
from pathlib import Path
import sys
source = Path(__file__).resolve().parents[4] / "docs/spec/retained9p-fixtures/cases.json"
cases = json.loads(source.read_text())["golden"]
names = ["hello", "create", "write", "mode"]
with Path(sys.argv[1]).open("w") as out:
    for name, case in zip(names, cases):
        body = json.dumps(case["body"], separators=(",", ":"), sort_keys=True)
        out.write("static const char golden_" + name + "[] = " +
                  json.dumps(body) + ";\n")

    def emit(name, body):
        if body.get("op") == "CALL":
            intent = json.dumps(body["payload"]["intent"], sort_keys=True,
                                separators=(",", ":")).encode()
            body["payload"]["claim"]["original_hash"] = hashlib.sha256(intent).hexdigest()
        value = json.dumps(body, sort_keys=True, separators=(",", ":"))
        out.write("static const char " + name + "[] = " + json.dumps(value) + ";\n")
    large = copy.deepcopy(cases[2]["body"])
    large["payload"]["intent"]["args"]["data"] = "ab" * 65536
    emit("large_write", large)
    drain = copy.deepcopy(large)
    drain["payload"]["intent"]["opcode"] = "DRAIN_WRITE"
    args = drain["payload"]["intent"]["args"]
    del args["max_end"]
    args.update(recall="00000000-0000-0000-0000-000000000099",
                dirty_entry="a" * 64, reservation="b" * 64)
    emit("large_drain", drain)
    bad = copy.deepcopy(cases[1]["body"])
    bad["payload"]["intent"]["args"]["umask"] = 512
    emit("bad_umask", bad)
    bad = copy.deepcopy(cases[3]["body"])
    bad["payload"]["intent"]["args"]["mode"] = 4096
    emit("bad_mode", bad)
    bad = copy.deepcopy(cases[2]["body"])
    del bad["payload"]["intent"]["args"]["max_end"]
    emit("missing_max_end", bad)
    bad = copy.deepcopy(cases[2]["body"])
    bad["payload"]["intent"]["args"]["max_end"] = 2**63
    emit("bad_max_end", bad)

    views = json.loads((source.parent / "views.json").read_text())
    for case in views["commands"]:
        emit("view_" + case["name"], case["body"])

    controlled = json.loads((source.parent / "controlled.json").read_text())
    for case in controlled["commands"]:
        emit("controlled_" + case["name"], case["body"])
    cleanup = json.loads((source.parent / "cleanup.json").read_text())
    for case in cleanup["commands"]:
        emit("cleanup_" + case["name"].replace("-", "_"), case["body"])
    allocate3 = copy.deepcopy(next(c["body"] for c in controlled["commands"] if c["name"] == "allocate"))
    allocate3["protocol"] = cleanup["protocol"]
    emit("controlled_allocate3", allocate3)
    receipt = controlled["receipts"][0]
    out.write("static const char controlled_receipt[] = " + json.dumps(json.dumps(receipt,sort_keys=True,separators=(",", ":"))) + ";\n")
