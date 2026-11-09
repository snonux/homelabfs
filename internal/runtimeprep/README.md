# Controlled-runtime run preparation (zq3 G2/G3)

`cmd/hfs-runtime-prep` prepares the inert inputs of one disposable
[controlled runtime](../controlledruntime/runtime.md) run. It is a workstation
tool: it never reads the operator private key (only records its path for the
signer), contacts a host, starts a runtime, initialises a volume or signs.
Nothing it writes carries native, guest, readiness or approval authority.
Preparation (s34) used no SSH, lab, VM, mount, install or private key.

## G2: generate

```
hfs-runtime-prep generate --spec /private/spec.json --output /private/new-bundle
```

The spec is a closed JSON schema (`Spec` in `spec.go`, exact Go field names,
duplicate/unknown/aliased keys refused) with every runtime capacity explicit;
[`testdata/zq3-spec.json`](testdata/zq3-spec.json) is a zq3-shaped example
that passes the actual runtime validation. The fresh `0700` bundle contains:

| File | Content |
|---|---|
| `runtime-config.json` | `homelabfs-controlled-runtime/1` config: two scopes, host receipt private keys, guest/operator public keys, resolved exporter argv |
| `keys/{host,guest}-N.key` | fresh Ed25519 seeds (lowercase hex, no newline: the signer's key form) |
| `volume-init.json` | G3 plan: helper pin, volume config/control/volume/mountpoint paths, fresh volume UUID |
| `request-template.json` | every signer field known now (key paths, sessions, public keys, mounts without device name/control nonce, guest budget, operator key path) |
| `generation.json` | review record: hashes of all files, sessions, public keys, VM, volume, `OperatorPrivateKeyRead:false` |

Before writing anything, the generated config goes through the **actual**
`controlledruntime.Run` on Linux (strict file decoder plus
`validateRuntimeConfig`, which then refuses with `Unsupported` before creating
any owner) with only VolumeConfig/OwnerDirectory/EnrollmentFile swapped for a
private sandbox. The sandbox (a `.v…` directory holding a transient copy of
the config) is created beside `--output`, not in `$TMPDIR`, and removed
afterwards. Because the runtime also checks the sandbox's lifecycle socket
against `sockaddr_un`, the parent of `--output` must be at most about 75
characters long; a longer one refuses with a clear message. The generator itself checks the real paths (clean, absolute,
disjoint, lifecycle socket within `sockaddr_un`) and the view controller's
enrollment-time work rule, which the runtime applies only inside the bootstrap
window. It also checks `Budget.Guest` against the guest floors
(`guestfloor.go`, w54), which the runtime does not check at all. zq3-r3's
`reservations` 32 failed every mount at bind_super. Two terminal mounts need
reservations ≥ 380 and received ≥ 60; the kernel provider refuses less at
provisioning. They also need about 280 MiB control and 242 MiB ordinary bytes,
a floor measured from the guest accounting, which the kernel enforces only at
mount. The example spec uses 1024/256 slots and 512/512 MiB, which raises
the guest accounting checked against `GuestBytes` from 772.5 to 1665.25 of
2048 MiB. On FreeBSD `generate` refuses: there `Run` would continue natively.
Build the tool from a commit whose `internal/` equals the candidate source
`e0cca0c` (true for `main` at `975cf80`); `generation.json` records the VCS
revision when stamped. The w54 floors change only `internal/runtimeprep`,
which the runtime daemon does not import, so its validation is unchanged.

Scopes: Session, GuestMount, Cluster, Export, OriginNode/OriginIncarnation,
user namespace, realm and ticket anchor/nonce are fresh UUIDs; VM is the argv
`-U` UUID; Volume is the planned volume. Configuration, Generation,
OriginJournal, ExporterIncarnation and FuseSession stay empty for the runtime's
actual proposal; GuestBoot stays empty for the signer. Authority/policy/registry
hashes are domain-separated run labels (the controlled profile has no normal
authority). OriginNode is not daemoncli's `node.json` ID; no controlled check
compares them. The mount idmap hash is SHA256 of the only admitted map,
`{"gid":[[0,0,4294967295]],"uid":[[0,0,4294967295]]}`.

### Exporter argv and the G4 record

The exporter argv is owned by G4
([`tools/lab/directlaunch`](../../tools/lab/directlaunch/README.md)), whose
`argv.py generate` needs the session UUIDs, while this generator draws them.
The order is therefore:

1. Spec `Exporter.Args` = the G4 controlled argv for the run and tap (the
   example spec holds the `zq3-r1`/`tap100` one), with `{{session:0}}` and
   `{{session:1}}` as the hfs0/hfs1 `hfs-controller` values.
   `hfs-runtime-prep generate` substitutes its fresh sessions.
2. `argv.py generate --session <hfs0> --session <hfs1>` with the sessions from
   `generation.json` (`Sessions[].DeviceTag`/`Session`).
3. Both must pass, and neither file is ever edited by hand:

```
python3 -I -B tools/lab/directlaunch/argv.py verify --record bhyve-argv.json \
  --record-sha256 SHA --runtime-config BUNDLE/runtime-config.json
hfs-runtime-prep check-argv --bundle BUNDLE --record bhyve-argv.json --record-sha256 SHA
```

`check-argv` requires the pinned record bytes, the bundle's run ID, the same
session for each share tag, the same exporter path and digest, the config's
`Exporter.Args` equal to the record's `args` element by element and in order,
and `args_sha256` recomputed as argv.py's canonical JSON. A mismatch means
regenerating from a corrected spec (new sessions, then a new record). The
test suite builds a genuine record with argv.py's own `build_record` from the
generated sessions and runs both checks.

`Exporter.Args` (without argv[0]) is parsed with bhyve's own getopt string.
It must define exactly two controlled 9p devices in this form:

```
-s 0:7:0,virtio-9p -o pci.0.7.0.sharename=hfs0 -o pci.0.7.0.hfs-controller={{session:0}}
-s 0:8:0,virtio-9p -o pci.0.8.0.sharename=hfs1 -o pci.0.8.0.hfs-controller={{session:1}}
```

bhyve's virtio-9p legacy option parser treats every `name=value` inside
`-s B:S:F,virtio-9p,...` as a share name, so the inline form
`virtio-9p,sharename=hfs0,hfs-controller=UUID` fails in bhyve ("more than one
share name") and is refused here. Also refused: stock/path or `ro` 9p exports,
`-k` config files, a PCI `-o` node before its `-s` or a `device` override, a
repeated slot, placeholders elsewhere, missing/invalid `-U`, more or fewer than
one VM name, more than 64 arguments or 64 KiB.

### Capacity notes

The admission package caps the runtime's aggregate pools at 4 GiB, so
`Budget.HostBytes` cannot exceed 4 GiB although the xz3 installation proposal
describes a 6 GiB host envelope. The example uses 4 GiB with a 1 GiB service
reservation. `Views` uses 6 MiB sources (room for the 4 MiB canary plus
metadata). The controller rule is `WorkBytes >= 3*LedgerBytes + 6*SourceBytes
+ 128 MiB`, so with the example's 44 MiB ledger `WorkBytes` must be at least
296 MiB; the example uses 320 MiB.

## Filling the signer requests (after the runtime started)

```
hfs-runtime-prep enroll-request --template BUNDLE/request-template.json \
  --proposal /private/proposal-copy.json --boot-id UUID --provider-nonce UUID \
  --device-names virtioA,virtioB --observer /private/observer.json --output /private/enroll-request.json
hfs-retained9p-operator enroll --request /private/enroll-request.json --output /private/new-enrollment
hfs-runtime-prep bind-request --template BUNDLE/request-template.json \
  --provision /private/new-enrollment/provision.json --awaiting /private/awaiting-guest-bind.json \
  --output /private/bind-request.json
hfs-retained9p-operator bind --request /private/bind-request.json --output /private/new-binding
```

`enroll-request` refuses a proposal whose sessions, host/guest keys, volume or
domain order differ from this generation, a proposal boot other than the
measured one and ticket families that collide with the ordinary family. It
copies each proposal control nonce into its mount. Device names are the guest's
actual 9p virtio devices in domain order (`virtioN`, for example the directory
under `/sys/bus/virtio/drivers/9pnet_virtio/` whose `mount_tag` is `hfs0`).
The observer input is closed JSON (`ObserverInput`, version
`homelabfs-runtime-observer-input/1`) with hex hashes of the f3 SSH binary,
pinned Rocky known-hosts file and installed-admission manifest (G5), the
run-only identity path (G6) and the guest address. `bind-request` reads the
runtime's `AWAITING_GUEST_BIND` stdout line, requires `publication:null`,
recomputes the provision payload hash exactly as the runtime does and refuses a
mismatch, then takes the group ID from the single original group. Measuring
the boot/nonce/device names and transferring files remain G1 duties.

## G3: source-matching volume/node initialisation

Chosen: a **separate pinned helper**, not a runtime change, so the xz3
candidate needs no rebuild. The helper is the actual `cmd/homelabfs`
(`node-init`, `volume-init --disposable-validation`, `inspect`) built from the
candidate source commit:

```
tools/lab/build-init-helper.sh /private/new-helper-dir
```

It builds FreeBSD/amd64 with `CGO_ENABLED=0`, `-trimpath`, `GOPROXY=off`,
pinned `GOAMD64=v1` and `GOFIPS140=off` and the caller's `GOEXPERIMENT`
removed (the toolchain default, `nodwarf5`, is recorded), twice with
independent caches, and requires identical bytes. A rebuild with hostile
`GOAMD64`/`GOEXPERIMENT`/`GOFIPS140` exported gave the same hash. On 2026-11-08 it
reproduced SHA256
`8c02b98fca3781cba6d64e9e5ba6caa0bb70b461298f85805bef2e77dd2c6e80` (Go
`go1.26.10-X:nodwarf5`; the same go.mod/go.sum hashes as the candidate daemon).
It is only ever used for these three commands, never as a daemon; it is not the
quarantined CGO-disabled runtime.

On f3, as root, after staging the helper under the private prefix (`0500`, one
link):

```
/usr/local/bin/python3.12 -I -B tools/lab/runtime-volume-init.py \
  --plan BUNDLE-COPY/volume-init.json --plan-sha256 SHA256 --record NEW_RECORD
```

`runtime-volume-init.py` checks the plan pin and closed schema, requires every
target to be new under owner-controlled private parents, hashes the helper
(root-controlled ancestors, single link) before and after, writes an intent
record, runs node-init, writes the daemoncli config (planned volume UUID plus
the actual node ID), runs volume-init and inspect (identical output required),
creates the empty `0700` mountpoint the FUSE shell requires and writes a result
record with file hashes. Each command has a 30 s deadline and a 64 KiB output
cap; on a deadline only that exact helper child is killed and the attempt is
`uncertain`. Nothing is deleted on failure; retries need a new run directory.

## Local evidence

`go test ./internal/runtimeprep` generates from the example spec and validates
through the actual runtime entry point, initialises the planned volume with the
same daemoncli code and opens it with `daemoncli.OpenControlled` and the
generated limits (service volume equals the scope volume), and drives both fill
steps through the actual external signer with a throwaway operator key, checking
the signed bind's provision hash. It builds a G4 record with argv.py's own
`build_record` from the generated sessions and passes both `check-argv` and
`argv.py verify` (when `python3` is available; otherwise that test skips).
Negative cases cover every generator, argv, request, check-argv and
runtime-owned refusal listed above, including the 64-argument and 64 KiB
argv bounds. `tools/lab/test_runtime_volume_init.py`
covers the helper with a fake for failure paths and, with
`HFS_HOMELABFS_TEST_BINARY`, a real local `cmd/homelabfs`. Root, the f3
filesystem and the runtime's actual proposal/native identities are declared
boundaries; nothing here proves native FreeBSD behavior.
