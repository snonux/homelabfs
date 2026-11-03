#!/usr/bin/env python3
"""Check the retained-9p design's bounded examples; never contact a runtime."""

import copy
import hashlib
import json
from pathlib import Path
import re
import struct
import sys
import uuid


HEADER = struct.Struct("<8sHHHHIIII16sQ32s")
CHUNK = 32768
STRIDE = 2097152
MAX_BODY = 1048576
FEATURES = {
    "body_schema": 1, "bridge_major": 1, "bridge_minor": 0,
    "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0,
}
CONTEXT_UUIDS = {
    "cluster", "configuration", "export", "exporter_incarnation", "fuse_session",
    "generation", "guest_boot", "guest_mount", "origin_incarnation",
    "origin_journal", "origin_node", "session", "vm", "volume",
}
CONTEXT_HASHES = {"authority_hash", "policy_hash", "registry_hash"}


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def fields(value, names):
    require(type(value) is dict and set(value) == set(names), "fields")


def integer(value, minimum, maximum):
    require(type(value) is int and minimum <= value <= maximum, "integer")


def identifier(value):
    require(type(value) is str, "uuid")
    try:
        parsed = uuid.UUID(value)
    except (ValueError, AttributeError):
        raise ValueError("uuid") from None
    require(parsed.int != 0 and str(parsed) == value, "uuid")


def hexbytes(value, maximum, exact=None):
    require(type(value) is str and re.fullmatch(r"(?:[0-9a-f]{2})*", value)
            is not None, "hex")
    size = len(value) // 2
    require(size <= maximum and (exact is None or size == exact), "hex-length")


def digest(value):
    hexbytes(value, 32, 32)


def canonical(value):
    # Bodies intentionally contain ASCII strings only. Bytes use lowercase hex.
    if type(value) is str:
        require(all(32 <= ord(c) <= 126 for c in value), "ascii")
    elif type(value) is list:
        for item in value:
            canonical(item)
    elif type(value) is dict:
        for key, item in value.items():
            require(type(key) is str, "key")
            canonical(key)
            canonical(item)
    else:
        require(value is None or type(value) in (bool, int), "json-type")
    return json.dumps(value, ensure_ascii=True, sort_keys=True,
                      separators=(",", ":"), allow_nan=False).encode("ascii")


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate-key")
        result[key] = value
    return result


def decode_body(raw):
    require(0 < len(raw) <= MAX_BODY, "body-length")
    try:
        value = json.loads(raw.decode("ascii"), object_pairs_hook=no_duplicates)
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError):
        raise ValueError("json") from None
    require(canonical(value) == raw, "noncanonical")
    return value


def context(value):
    fields(value, CONTEXT_UUIDS | CONTEXT_HASHES)
    for name in CONTEXT_UUIDS:
        identifier(value[name])
    for name in CONTEXT_HASHES:
        digest(value[name])


def credentials(value):
    fields(value, {"fsuid", "fsgid", "groups", "process", "credential_revision",
                   "idmap_hash", "user_namespace"})
    integer(value["fsuid"], 0, 2**32 - 1)
    integer(value["fsgid"], 0, 2**32 - 1)
    integer(value["credential_revision"], 1, 2**64 - 1)
    identifier(value["process"])
    identifier(value["user_namespace"])
    digest(value["idmap_hash"])
    groups = value["groups"]
    require(type(groups) is list and len(groups) <= 1024, "groups-bound")
    for group in groups:
        integer(group, 0, 2**32 - 1)
    require(groups == sorted(set(groups)), "groups-order")


def claim(value):
    fields(value, {"realm", "subject", "anchor", "nonce", "number", "original_hash"})
    for name in ("realm", "anchor", "nonce"):
        identifier(value[name])
    hexbytes(value["subject"], 1024)
    require(value["subject"] != "", "subject")
    integer(value["number"], 1, 2**64 - 1)
    digest(value["original_hash"])


def capability(value, scope):
    fields(value, {"token", "kind", "scope", "object_home", "object_allocation",
                   "description", "rights", "revision", "credential_hash",
                   "ancestors_hash"})
    for name in ("token", "credential_hash", "ancestors_hash"):
        digest(value[name])
    context(value["scope"])
    require(value["scope"] == scope, "capability-scope")
    for name in ("object_home", "object_allocation"):
        identifier(value[name])
    require(value["kind"] in ("ROOT", "PATH", "DESCRIPTION", "DOMAIN"), "cap-kind")
    if value["kind"] == "DESCRIPTION":
        identifier(value["description"])
    else:
        require(value["description"] is None, "description")
    integer(value["rights"], 0, 63)
    integer(value["revision"], 1, 2**64 - 1)


def command(value):
    """The checked semantic subset is HELLO, CREATE, WRITE and native DRAIN flags."""
    fields(value, {"cell", "context", "op", "payload", "reclaim"})
    integer(value["cell"], 1, 2**31 - 1)
    context(value["context"])
    if value["reclaim"] is not None:
        fields(value["reclaim"], {"cell", "response_hash"})
        integer(value["reclaim"]["cell"], 1, 2**31 - 1)
        require(value["reclaim"]["cell"] != value["cell"], "reclaim-self")
        digest(value["reclaim"]["response_hash"])
    if value["op"] == "HELLO":
        fields(value["payload"], FEATURES)
        for name, expected in FEATURES.items():
            integer(value["payload"][name], expected, expected)
        return
    require(value["op"] == "CALL", "checker-operation-subset")
    payload = value["payload"]
    fields(payload, {"claim", "credentials", "intent"})
    claim(payload["claim"])
    credentials(payload["credentials"])
    intent = payload["intent"]
    fields(intent, {"opcode", "args"})
    require(hashlib.sha256(canonical(intent)).hexdigest() ==
            payload["claim"]["original_hash"], "original-hash")
    args = intent["args"]
    if intent["opcode"] == "CREATE":
        fields(args, {"parent", "name", "mode", "umask", "flags"})
        capability(args["parent"], value["context"])
        require(args["parent"]["kind"] in ("ROOT", "PATH"), "parent-kind")
        hexbytes(args["name"], 255)
        require(args["name"] != "" and bytes.fromhex(args["name"]) not in
                (b".", b"..") and not any(c in bytes.fromhex(args["name"])
                                          for c in (0, 47)), "name")
        integer(args["mode"], 0, 4095)
        integer(args["umask"], 0, 511)
        flags = args["flags"]
        fields(flags, {"access", "append", "create", "exclusive", "sync", "truncate"})
        require(flags["access"] in ("READ", "WRITE", "READWRITE"), "access")
        require(flags["sync"] in ("ORDINARY", "DATA", "FILE"), "sync")
        for name in ("append", "create", "exclusive", "truncate"):
            require(type(flags[name]) is bool, "boolean")
    elif intent["opcode"] in ("WRITE", "DRAIN_WRITE"):
        keys = {"description", "offset", "data"}
        if intent["opcode"] == "WRITE":
            keys.add("max_end")
            integer(args["max_end"], 0, 2**63 - 1)
        if intent["opcode"] == "DRAIN_WRITE":
            keys |= {"recall", "dirty_entry", "reservation"}
            identifier(args["recall"])
            digest(args["dirty_entry"])
            digest(args["reservation"])
        fields(args, keys)
        capability(args["description"], value["context"])
        require(args["description"]["kind"] == "DESCRIPTION" and
                args["description"]["rights"] & 2, "write-grant")
        integer(args["offset"], 0, 2**63 - 1)
        hexbytes(args["data"], 65536)
    else:
        raise ValueError("checker-opcode-subset")


def frames(body, native_flags=0):
    raw = canonical(body)
    require(0 < len(raw) <= MAX_BODY, "body-length")
    session = uuid.UUID(body["context"]["session"]).bytes
    return [HEADER.pack(b"HFS9BR1\0", 1, 0, 1, native_flags, len(raw), offset,
                        len(raw[offset:offset + CHUNK]), 0, session,
                        body["cell"], hashlib.sha256(raw).digest()) +
            raw[offset:offset + CHUNK] for offset in range(0, len(raw), CHUNK)]


def assemble(chunks, expected_session, cell, native=False):
    parts = {}
    identity = None
    for frame in chunks:
        require(len(frame) >= HEADER.size, "short-header")
        magic, major, minor, kind, flags, total, offset, length, reserved, session, \
            frame_cell, body_hash = HEADER.unpack(frame[:HEADER.size])
        require((magic, major, minor, kind, reserved) ==
                (b"HFS9BR1\0", 1, 0, 1, 0), "header")
        require(flags & ~3 == 0 if native else flags == 0, "header")
        require(session == uuid.UUID(expected_session).bytes and frame_cell == cell,
                "frame-scope")
        require(0 < total <= MAX_BODY and offset < total and offset % CHUNK == 0
                and length == min(CHUNK, total - offset), "chunk-bounds")
        require(len(frame) == HEADER.size + length, "frame-length")
        current = (total, body_hash, flags)
        require(identity is None or identity == current, "changed-assembly")
        identity = current
        part = frame[HEADER.size:]
        require(offset not in parts or parts[offset] == part, "changed-chunk")
        parts[offset] = part
    require(identity is not None, "empty")
    total, body_hash, flags = identity
    require(set(parts) == set(range(0, total, CHUNK)), "incomplete")
    raw = b"".join(parts[offset] for offset in sorted(parts))
    require(hashlib.sha256(raw).digest() == body_hash, "body-hash")
    value = decode_body(raw)
    require(value["cell"] == cell and value["context"]["session"] == expected_session,
            "body-scope")
    command(value)
    if native:
        expected_control = value["op"] not in ("HELLO", "CALL")
        if value["op"] == "CALL":
            expected_control = value["payload"]["intent"]["opcode"] == "DRAIN_WRITE"
        if value["op"] != "RELEASE_CELL":
            require(bool(flags & 1) == expected_control, "native-credit-class")
        require(bool(flags & 2) == (value["reclaim"] is not None), "native-reclaim")
    return value


def guest_send(frame, cell, tag=0):
    payload = struct.pack("<BBQI", 1, 1, cell, 0) + frame
    return struct.pack("<IBH", 7 + len(payload), 240, tag) + payload


def check_guest_send(raw):
    require(7 + 14 + HEADER.size <= len(raw) <= 65536, "9p-length")
    length, kind, tag = struct.unpack("<IBH", raw[:7])
    require(length == len(raw) and kind == 240 and tag != 65535, "9p-header")
    method, channel, cell, chunk = struct.unpack("<BBQI", raw[7:21])
    require((method, channel) == (1, 1), "9p-method-channel")
    frame = raw[21:]
    values = HEADER.unpack(frame[:HEADER.size])
    require(values[10] == cell and values[6] == chunk * CHUNK, "9p-frame-selector")
    require(len(frame) == HEADER.size + values[7], "9p-frame-length")


def changed(base, updates):
    value = copy.deepcopy(base)
    for path, replacement in updates:
        target = value
        for part in path[:-1]:
            target = target[part]
        target[path[-1]] = replacement
    return value


def reject(action, reason):
    try:
        action()
    except ValueError as error:
        require(str(error) == reason, f"expected {reason}, received {error}")
    else:
        raise ValueError(f"accepted malformed example: {reason}")


def history(steps):
    """A tiny claim/ack example interpreter, not the all-origin state machine."""
    retained = {}
    retired = set()
    for step in steps:
        fields(step, {"action", "key", "intent", "expected", "origin", "expected_origin"})
        key, intent, action = step["key"], step["intent"], step["action"]
        if action == "call":
            if key in retired:
                outcome = "RETIRED_ID"
            elif key in retained:
                outcome = "REPLAY" if retained[key][0] == intent else "ID_CONFLICT"
            else:
                retained[key] = (intent, step["origin"])
                outcome = "ADMITTED"
        elif action in ("cancel", "disconnect", "query"):
            outcome = "INDETERMINATE" if key in retained else "UNKNOWN"
        elif action == "retire-pending":
            outcome = "BLOCKED" if key in retained else "UNKNOWN"
        elif action == "terminal-and-retire":
            require(key in retained, "retire-without-claim")
            retired.add(key)
            outcome = "RETIRED"
        else:
            raise ValueError("history-action")
        require(outcome == step["expected"], f"history: {step}")
        require(retained[key][1] == step["expected_origin"], "retry-moved-origin")


def native_cells(example):
    """Model endpoint-wide positional selection independently of a read's session."""
    endpoint = example["endpoint"]
    identifier(example["exporter_incarnation"])
    identifier(endpoint["fuse_session"])
    high_water = 0
    cells = {}
    forwarding = {}
    active = set(example["sessions"])
    continuity = True

    def selected(native_cell):
        # A native pread has no guest-session selector. Only its endpoint/offset
        # chooses a response; checking a returned session cannot fix a collision.
        require(native_cell in cells, "native-cell-missing")
        require(not cells[native_cell]["released"], "released-cell")
        return cells[native_cell]["response"]

    for step in example["steps"]:
        action = step["action"]

        def apply():
            nonlocal high_water, continuity
            require(step["endpoint"] == endpoint, "endpoint-scope")
            if action == "allocate":
                require(continuity, "ledger-missing")
                require(step["session"] in active, "session-inactive")
                key = (step["session"], step["guest_cell"])
                require(key not in forwarding, "duplicate-guest-cell")
                native_cell = step["native_cell"]
                require(native_cell not in cells and native_cell > high_water,
                        "native-cell-reuse")
                require(native_cell == high_water + 1, "native-cell-gap")
                require(native_cell <= 2**31 - 1, "native-cell-exhausted")
                high_water = native_cell
                forwarding[key] = native_cell
                cells[native_cell] = {"session": step["session"],
                                      "response": step["response"], "released": False}
                return "ALLOCATED"
            if action == "native-read":
                require(step["offset"] % STRIDE == 0, "example-read-offset")
                return selected(step["offset"] // STRIDE)
            if action in ("fetch", "release"):
                require(step["session"] in active, "session-inactive")
                key = (step["session"], step["guest_cell"])
                require(key in forwarding, "mapping-missing")
                native_cell = forwarding[key]
                require(cells[native_cell]["session"] == step["session"], "cell-session")
                response = selected(native_cell)
                if action == "fetch":
                    return response
                require(response == step["response"], "response-identity")
                cells[native_cell]["released"] = True
                return "RELEASED"
            if action == "native-release":
                require(step["session"] in active, "session-inactive")
                native_cell = step["native_cell"]
                require(native_cell in cells, "native-cell-missing")
                require(cells[native_cell]["session"] == step["session"], "cell-session")
                require(selected(native_cell) == step["response"], "response-identity")
                cells[native_cell]["released"] = True
                return "RELEASED"
            if action == "replace":
                require(continuity and step["ledger_complete"], "ledger-missing")
                require(step["high_water"] == high_water, "allocator-continuity")
                require(step["old_session"] in active and step["session"] not in active,
                        "replacement-session")
                active.remove(step["old_session"])
                active.add(step["session"])
                return "RECONCILED"
            if action == "forget-mapping":
                key = (step["session"], step["guest_cell"])
                require(key in forwarding, "mapping-missing")
                del forwarding[key]
                continuity = False
                return "CONTINUITY_LOST"
            if action == "reset":
                require(step["high_water"] == high_water, "allocator-reset")
                return "UNCHANGED"
            raise ValueError("native-cell-action")

        if "error" in step:
            # Rejection must preserve both allocation and response ownership.
            before = copy.deepcopy((high_water, cells, forwarding, active, continuity))
            reject(apply, step["error"])
            require(before == (high_water, cells, forwarding, active, continuity),
                    "failed-cell-action-mutated")
        else:
            require(apply() == step["expected"], f"native-cell case: {step['name']}")
    return len(example["steps"])


def full_native_cells(example):
    """Check atomic credit donation with all 64+16 endpoint credits occupied."""
    cells = {entry["cell"]: copy.deepcopy(entry) for entry in example["initial"]}
    require(len(cells) == 80 and set(cells) == set(range(1, 81)), "full-cell-fixture")
    require(sum(entry["lane"] == "ordinary" for entry in cells.values()) == 64,
            "ordinary-credit-fixture")
    require(sum(entry["lane"] == "control" for entry in cells.values()) == 16,
            "control-credit-fixture")
    high_water = 80

    for step in example["steps"]:
        def apply():
            nonlocal high_water
            action = step["action"]
            if action == "read":
                require(step["cell"] in cells, "native-cell-missing")
                require(not cells[step["cell"]]["released"], "released-cell")
                return cells[step["cell"]]["response"]
            if action == "without-reclaim":
                require(sum(not row["released"] for row in cells.values()) < 80,
                        "cell-capacity")
                raise ValueError("fixture-not-full")
            require(action == "replace", "full-cell-action")
            target = step["reclaim"]["cell"]
            require(target in cells and not cells[target]["released"], "reclaim-target")
            old = cells[target]
            require(old["session"] == step["session"], "cell-session")
            require(old["received"], "snapshot-incomplete")
            require(hashlib.sha256(old["response"].encode("ascii")).hexdigest()
                    == step["reclaim"]["response_hash"], "response-identity")
            require(step["schema_valid"], "replacement-schema")
            require(step["resources_reserved"], "replacement-resources")
            new_cell = step["cell"]
            require(new_cell == high_water + 1 and new_cell not in cells,
                    "replacement-allocation")
            if step["operation"] == "RELEASE_CELL":
                require(step["payload"] == step["reclaim"], "release-ack-mismatch")
                lane = old["lane"]
            else:
                lane = step["lane"]
                require(lane == old["lane"], "credit-class")
            # Validation finishes before any mutation. The fixed parser workspace
            # transfers one existing credit; it never installs an 81st live cell.
            old["released"] = True
            cells[new_cell] = {"cell": new_cell, "lane": lane,
                               "session": step["session"], "response": step["response"],
                               "received": True, "released": False}
            high_water = new_cell
            return "REPLACED"

        if "error" in step:
            before = copy.deepcopy((high_water, cells))
            reject(apply, step["error"])
            require(before == (high_water, cells), "failed-full-release-mutated")
        else:
            require(apply() == step["expected"], f"full-cell case: {step['name']}")
        require(sum(not row["released"] for row in cells.values()) == 80,
                "unbounded-extra-cell")
        require(sum(not row["released"] and row["lane"] == "ordinary"
                    for row in cells.values()) == 64, "ordinary-credit-changed")
        require(sum(not row["released"] and row["lane"] == "control"
                    for row in cells.values()) == 16, "control-credit-changed")
    return len(example["steps"])


def check_native_flags(fixture):
    """Native-only metadata; guest goldens stay byte-identical."""
    count = 0
    base = copy.deepcopy(fixture["golden"][2]["body"])
    base["payload"]["intent"]["args"]["data"] = "ab" * 65536
    for case in fixture["native_flag_cases"]:
        body = copy.deepcopy(base)
        intent = body["payload"]["intent"]
        intent["opcode"] = case["opcode"]
        if case["opcode"] == "DRAIN_WRITE":
            intent["args"].pop("max_end", None)
            intent["args"].update(recall=str(uuid.UUID(int=70)),
                                  dirty_entry="11" * 32, reservation="22" * 32)
        body["payload"]["claim"]["original_hash"] = hashlib.sha256(canonical(intent)).hexdigest()
        if case["reclaim"]:
            body["reclaim"] = {"cell": 1, "response_hash": "33" * 32}
        chunks = frames(body, case["flags"])
        require(len(chunks) > 1, "native-fragment-fixture")
        if case["changed_chunk_flags"]:
            changed = bytearray(chunks[-1])
            changed[14] ^= 1
            chunks[-1] = bytes(changed)
        action = lambda: assemble(chunks, body["context"]["session"], body["cell"], native=True)
        if case["error"] is None:
            require(action() == body, "native-flags-roundtrip")
        else:
            reject(action, case["error"])
        count += 1
    guest = frames(fixture["golden"][0]["body"], 1)
    reject(lambda: assemble(guest, fixture["golden"][0]["body"]["context"]["session"],
                            fixture["golden"][0]["body"]["cell"]), "header")
    return count + 1


def main():
    path = Path(__file__).resolve().parents[2] / "docs/spec/retained9p-fixtures/cases.json"
    fixture = json.loads(path.read_text(encoding="ascii"), object_pairs_hook=no_duplicates)
    count = 0
    for example in fixture["golden"]:
        body = example["body"]
        encoded = frames(body)
        require([part.hex() for part in encoded] == example["frame_hex"], "golden-bytes")
        require(hashlib.sha256(canonical(body)).hexdigest() == example["body_hash"],
                "golden-hash")
        require(assemble(encoded[::-1] + encoded[:1], body["context"]["session"],
                         body["cell"]) == body, "golden-roundtrip")
        count += 1
    base = fixture["golden"][1]["body"]
    for example in fixture["malformed"]:
        body = changed(base, example["updates"])
        # Some cases intentionally change semantic fields while retaining an old
        # identity; others recalculate the intent hash to test a deeper boundary.
        if example["rehash"]:
            body["payload"]["claim"]["original_hash"] = hashlib.sha256(
                canonical(body["payload"]["intent"])).hexdigest()
        reject(lambda: command(body), example["error"])
        count += 1
    for example in fixture["raw_malformed"]:
        reject(lambda: decode_body(bytes.fromhex(example["hex"])), example["error"])
        count += 1
    for example in fixture["frame_malformed"]:
        chunks = [bytes.fromhex(part) for part in fixture["golden"][0]["frame_hex"]]
        first = bytearray(chunks[0])
        first[example["offset"]] = example["byte"]
        chunks[0] = bytes(first)
        body = fixture["golden"][0]["body"]
        reject(lambda: assemble(chunks, body["context"]["session"], body["cell"]),
               example["error"])
        count += 1
    hello = fixture["golden"][0]["body"]
    for example in fixture["negotiation_malformed"]:
        reject(lambda: command(changed(hello, example["updates"])), example["error"])
        count += 1
    wire = guest_send(frames(hello)[0], hello["cell"])
    require(wire.hex() == fixture["guest_send_hex"], "9p-golden")
    check_guest_send(wire)
    for example in fixture["guest_malformed"]:
        malformed = bytearray(wire)
        malformed[example["offset"]] = example["byte"]
        reject(lambda: check_guest_send(malformed), example["error"])
        count += 1
    group_example = copy.deepcopy(base)
    group_example["payload"]["credentials"]["groups"] = list(range(1024))
    command(group_example)
    group_example["payload"]["credentials"]["groups"].append(1024)
    reject(lambda: command(group_example), "groups-bound")
    # Exercise multipart response-selection arithmetic with a legal 64KiB write.
    write = fixture["golden"][2]["body"]
    write = copy.deepcopy(write)
    write["payload"]["intent"]["args"]["data"] = "ab" * 65536
    write["payload"]["claim"]["original_hash"] = hashlib.sha256(
        canonical(write["payload"]["intent"])).hexdigest()
    encoded = frames(write)
    assemble(encoded[::-1], write["context"]["session"], write["cell"])
    reject(lambda: assemble(encoded[:-1], write["context"]["session"], write["cell"]),
           "incomplete")
    for index, part in enumerate(encoded):
        position = write["cell"] * STRIDE + index * (HEADER.size + CHUNK)
        require(position // STRIDE == write["cell"] and len(part) <= CHUNK + HEADER.size,
                "cell-offset")
        # Native reads can split both the header and payload at arbitrary bytes.
        pieces = [part[offset:offset + 17] for offset in range(0, len(part), 17)]
        require(b"".join(pieces) == part, "short-read")
    history(fixture["history"])
    native_case_count = native_cells(fixture["native_cells"])
    full_case_count = full_native_cells(fixture["full_native_cells"])
    for example in fixture["acknowledgements"]:
        require((example["expected_context"] == example["received_context"])
                == example["accepted"], "ack-context")
    # Final create metadata is a literal expected selection, not a DAC algorithm.
    for final in fixture["create_selection"]:
        require(final["creator_fsuid"] == final["selected_uid"] and
                final["parent_gid"] == final["selected_gid"] and
                final["selected_mode"] == final["requested_mode"] and
                511 < final["selected_mode"] <= 4095, "create-selection")
    count += check_native_flags(fixture)
    print(f"PASS: {count} literal golden/malformed examples; groups1024 boundary; "
          f"{len(encoded)}-chunk write/short reads; claim history; "
          f"{len(fixture['acknowledgements'])} acknowledgement contexts; atomic-create example; "
          f"{native_case_count} endpoint-wide native-cell cases; "
          f"{full_case_count} full-capacity replacement cases")
    print("Design subset only: no runtime, authentication, normal3 proof, DAC, "
          "durability, cache revocation or FUSE compatibility claim.")


if __name__ == "__main__":
    try:
        main()
        from check_retained9p_views import check_views
        check_views()
    except (ValueError, KeyError, TypeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
