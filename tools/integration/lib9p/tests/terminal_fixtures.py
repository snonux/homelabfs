#!/usr/bin/env python3
"""Independent canonical mapping/manifest fixtures; signatures are deliberately
invalid zero bytes. These are exporter transport tests, never host authority."""
import copy
import hashlib
import json
from pathlib import Path
import sys

spec = Path(__file__).resolve().parents[4] / "docs/spec/retained9p-fixtures"
cases = json.loads((spec / "controlled.json").read_text())["commands"]
hello = next(c["body"] for c in cases if c["name"] == "hello")
allocate = next(c["body"] for c in cases if c["name"] == "allocate")
scope = hello["context"]
credentials = allocate["payload"]["credentials"]
protocol = dict(body_schema=5, bridge_major=1, bridge_minor=4, features=32767,
                msize=65536, normal_major=3, normal_minor=0)
endpoint = dict(fuse_session=scope["fuse_session"], command_node=110,
                result_node=111, recall_node=112)
controller = "00000000-0000-0000-0000-00000000002a"


def raw(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def digest(value):
    return hashlib.sha256(raw(value).encode()).hexdigest()


def command(cell, op, payload, reclaim=None):
    return dict(cell=cell, context=scope, delegation=None, op=op,
                payload=payload, protocol=protocol, reclaim=reclaim)


def native(guest, cell, donor=None):
    value = copy.deepcopy(guest)
    value["cell"] = cell
    value["reclaim"] = donor
    value["delegation"] = dict(guest_cell=guest["cell"], guest_op=guest["op"],
                               guest_reclaim=guest["reclaim"], poll_index=0,
                               version="homelabfs-native-delegation/1")
    return value


def chain(guest, translated, response_base):
    seed = digest(dict(domain="homelabfs-mapping-chain/1", endpoint=endpoint,
                       controller=controller, scope=scope,
                       guest_cell=guest["cell"], guest_command_hash=digest(guest)))
    return digest(dict(domain="homelabfs-mapping-leg/1", previous=seed, index=0,
                       native_cell=translated["cell"],
                       native_command_hash=digest(translated),
                       native_response_hash=digest(response_base)))


# A schema-shaped maximal READDIR result supplies legitimate large opaque native
# bodies to the exporter. Native I/O/capability signatures remain fixture data.
pressure = copy.deepcopy(json.loads((spec / "cases.json").read_text())["golden"][2]["body"])
pressure["cell"] = 1
cap = copy.deepcopy(pressure["payload"]["intent"]["args"]["description"])
cap.update(kind="PATH", description=None)
pressure["payload"]["intent"] = dict(opcode="READDIR", args=dict(object=cap, after=0, revision=1, limit=256))
pressure["payload"]["claim"]["original_hash"] = digest(pressure["payload"]["intent"])
attributes = dict(object_home=cap["object_home"], object_allocation=cap["object_allocation"],
                  kind="REGULAR", uid=2**32-1, gid=2**32-1, mode=4095,
                  size=2**63-1, nlink=2**64-1, revision=2**64-1,
                  atime=dict(seconds=2**63-1, nanos=999999999),
                  mtime=dict(seconds=2**63-1, nanos=999999999),
                  ctime=dict(seconds=2**63-1, nanos=999999999))

def pressure_response(name_length):
    entries = [dict(name=(f"{i:04x}" + "x" * (name_length - 4)).encode().hex(),
                    capability=cap, attributes=attributes) for i in range(256)]
    result = dict(id="f"*64, code="OK", count=256, data="", attributes=None,
                  capabilities=[], entries=entries, capacity=None, publication=None)
    return dict(cell=1, context=pressure["context"], op="CALL", payload=result)

base_reply_length = len(raw(pressure_response(4)))
request_length = len(raw(pressure))
choices = [(rows * (2 * request_length + 2 * (base_reply_length + 512 * (names - 4)) + 64), rows, names)
           for rows in range(1, 63) for names in range(4, 256)]
_, pressure_rows, pressure_name_length = max(c for c in choices if c[0] <= (48 << 20) - 32768)
pressure_reply = pressure_response(pressure_name_length)

with Path(sys.argv[1]).open("w") as out:
    def emit(name, value):
        out.write(f"static const char {name}[] = {json.dumps(raw(value))};\n")

    old = []
    for i in range(1, 81):
        op = "HELLO" if i <= 64 else "QUERY"
        payload = protocol if op == "HELLO" else {"claim": allocate["payload"]["claim"]}
        guest = command(i, op, payload)
        translated = native(guest, i)
        result = dict(selection=protocol, status="ACCEPTED") if op == "HELLO" else dict(status="RELEASED")
        reply = dict(cell=i, context=scope, op=op, payload=result,
                     protocol=protocol, verification=None)
        emit(f"terminal_old_guest_{i}", guest)
        emit(f"terminal_old_native_{i}", translated)
        emit(f"terminal_old_reply_{i}", reply)
        old.append((guest, translated, reply))
    out.write("static const char *const terminal_old_guest[] = {" +
              ",".join(f"terminal_old_guest_{i}" for i in range(1, 81)) + "};\n")
    out.write("static const char *const terminal_old_native[] = {" +
              ",".join(f"terminal_old_native_{i}" for i in range(1, 81)) + "};\n")
    out.write("static const char *const terminal_old_reply[] = {" +
              ",".join(f"terminal_old_reply_{i}" for i in range(1, 81)) + "};\n")
    for name, count, funding in [("empty", 0, 0), ("sole", 1, 1),
                                 ("ordinary", 65, 1), ("pressure", 1, 1), ("full", 80, 80)]:
        acks = [dict(cell=i, response_hash=digest(old[i-1][2]))
                for i in range(1, count + 1)]
        donor = acks[funding - 1] if funding else None
        settled = [a for a in acks if a is not donor]
        payload = dict(retirement_id="00000000-0000-0000-0000-000000000099",
                       credentials=credentials, guest_through=count,
                       observer_revision=1, closure_hash="2" * 64,
                       settled_cells=settled)
        guest = command(count + 1, "SESSION_RETIRE", payload, donor)
        native_cell = pressure_rows + 2 if name == "pressure" else count + 1
        translated = native(guest, native_cell, donor)
        result = dict(status="RETIRED", retirement_id=payload["retirement_id"],
                      guest_through=count, observer_revision=1,
                      closure_hash=payload["closure_hash"],
                      settled_hash=digest(settled), ownership_hash="3" * 64)
        base = dict(cell=native_cell, context=scope, op="SESSION_RETIRE",
                    payload=result, protocol=protocol)
        evidence = {k: v for k, v in payload.items() if k != "settled_cells"}
        evidence.update(settled_hash=digest(settled), ownership_hash="3" * 64,
                        retained_owner="C8_TERMINAL_CELL")
        receipt = dict(version="homelabfs-controlled-verification/2",
                       kind="SESSION_RETIRED", scope=scope, endpoint=endpoint,
                       controller=controller,
                       serving_generation="00000000-0000-0000-0000-000000000088",
                       cell=native_cell, command_hash=digest(translated),
                       response_hash=digest(base), evidence=evidence,
                       signature="0" * 128, guest_cell=count + 1,
                       guest_command_hash=digest(guest),
                       guest_response_hash=digest(dict(base, cell=count + 1)),
                       poll_count=1, mapping_hash=chain(guest, translated, base))
        reply = dict(base, verification=receipt)
        emit(f"terminal_{name}_guest", guest)
        emit(f"terminal_{name}_native", translated)
        emit(f"terminal_{name}_reply", reply)
        if name == "pressure":
            emit("terminal_pressure_guest_reply", dict(reply, cell=count + 1))
    # Each closed-schema/manifest violation is applied to the full80-cell case.
    bad = []
    for name in ("missing", "duplicate", "omitted", "funding", "future",
                 "hash", "unsorted", "overflow", "no_donor", "unknown"):
        value = copy.deepcopy(guest)
        manifest = value["payload"]["settled_cells"]
        if name == "missing":
            del value["payload"]["settled_cells"]
        elif name == "duplicate":
            manifest[1] = copy.deepcopy(manifest[0])
        elif name == "omitted":
            manifest.pop()
        elif name == "funding":
            manifest[-1] = copy.deepcopy(value["reclaim"])
        elif name == "future":
            manifest[-1]["cell"] = value["cell"]
        elif name == "hash":
            manifest[0]["response_hash"] = "0" * 64
        elif name == "unsorted":
            manifest[0], manifest[1] = manifest[1], manifest[0]
        elif name == "overflow":
            manifest.append(copy.deepcopy(value["reclaim"]))
        elif name == "no_donor":
            value["reclaim"] = None
        else:
            value["payload"]["unknown"] = 0
        emit("terminal_bad_" + name, value)
        bad.append("terminal_bad_" + name)
    out.write("static const char *const terminal_bad_guest[] = {" +
              ",".join(bad) + "};\n")

    emit("terminal_pressure_ordinary", pressure)
    emit("terminal_pressure_ordinary_reply", pressure_reply)
    out.write(f"static const unsigned terminal_pressure_rows = {pressure_rows};\n")

    maximum_scope = {k: "f" * 64 if k in ("authority_hash", "policy_hash", "registry_hash")
                     else "ffffffff-ffff-ffff-ffff-ffffffffffff" for k in scope}
    maximum_credentials = dict(credentials, credential_revision=2**64-1,
                               fsgid=2**32-1, fsuid=2**32-1,
                               groups=list(range(2**32-1024, 2**32)),
                               idmap_hash="f"*64,
                               process="ffffffff-ffff-ffff-ffff-ffffffffffff",
                               user_namespace="ffffffff-ffff-ffff-ffff-ffffffffffff")
    maximum_payload = dict(retirement_id=maximum_scope["session"],
                           credentials=maximum_credentials,
                           guest_through=2147483646, observer_revision=2**64-1,
                           closure_hash="f"*64,
                           settled_cells=[dict(cell=2147483567+i, response_hash="f"*64)
                                          for i in range(79)])
    maximum_ack = dict(cell=2147483646, response_hash="f"*64)
    maximum_guest = dict(cell=2147483647, context=maximum_scope, delegation=None,
                         op="SESSION_RETIRE", payload=maximum_payload,
                         protocol=protocol, reclaim=maximum_ack)
    maximum_native = native(maximum_guest, 2147483647, maximum_ack)
    maximum_result = dict(status="RETIRED", retirement_id=maximum_scope["session"],
                          guest_through=2147483646, observer_revision=2**64-1,
                          closure_hash="f"*64, settled_hash="f"*64, ownership_hash="f"*64)
    maximum_evidence = {k: v for k, v in maximum_payload.items() if k != "settled_cells"}
    maximum_evidence.update(settled_hash="f"*64, ownership_hash="f"*64,
                            retained_owner="C8_TERMINAL_CELL")
    maximum_receipt = dict(receipt, scope=maximum_scope,
                           endpoint=dict(fuse_session=maximum_scope["fuse_session"],
                                         command_node=2**64-1, result_node=2**64-1,
                                         recall_node=2**64-1),
                           controller=maximum_scope["session"],
                           serving_generation=maximum_scope["session"],
                           cell=2147483647, guest_cell=2147483647,
                           evidence=maximum_evidence)
    maximum_reply = dict(cell=2147483647, context=maximum_scope,
                         op="SESSION_RETIRE", payload=maximum_result,
                         protocol=protocol, verification=maximum_receipt)
    emit("terminal_maximum_guest", maximum_guest)
    emit("terminal_maximum_native", maximum_native)
    emit("terminal_maximum_reply", maximum_reply)
