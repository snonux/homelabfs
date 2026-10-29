# Prepared lab admission driver

`tools/lab/prepared-lab-preflight.py` implements the first, read-only
`prepared-lab-preflight-v1` step of the existing [lab runner](Lab-runner.md).
It consumes the runner's canonical `Plan` snapshot and environment. The runner
continues to own approval, artifact verification, lifecycle, durable state,
cleanup and reporting. There is no standalone approval or execution coordinator.
The driver does not import or run a PoC.

This driver has **offline fixture validation only** and has not been executed
against the lab. Its parser fixture retains the actual FreeBSD `df -kP` header
from separately collected read-only planning evidence supplied by the root
agent. That sample is not a driver admission or native test pass. A real
admission and native acceptance run still require a concrete reviewed Plan and
Paul's fresh approval under [Test automation](Test-automation.md).

## Invocation and pins

The first Plan step must pin the executable Python file itself, declare
`phase: "setup"`, `action: "driver"`, `effects: "read-only"`, and
`contract: "prepared-lab-preflight-v1"`. Its exact argv is:

```json
["--policy-artifact", "preflight-policy"]
```

Use a 60–600 second step timeout. The policy artifact ID may differ, provided
that the reviewed argv and artifact ID agree. Pin all of these as ordinary
Plan artifacts with absolute controller paths, immutable version labels and
SHA-256 checksums:

- The executable `tools/lab/prepared-lab-preflight.py`.
- The admission policy JSON described below.
- A host known-hosts file containing the independently verified `f3` ed25519
  key. No host key is discovered, accepted or written automatically.
- A guest known-hosts file containing exactly the native guest pins from
  `docs/lab/f3-known_hosts`, plus a reviewed Rocky address/key when Rocky is
  initially running. Native address/key pairs must match the repository file.
- The current `tools/lab/vms.sh`, selected by the Plan's `start-lab` step.
  The driver also requires its exact reviewed checksum and repository path;
  an older helper with the same basename cannot satisfy this dependency.
  Changes to this helper require reviewing/updating the driver's
  `GUARDED_VMS_SHA256` constant and re-pinning both artifacts.
- All scenario-specific inputs already required by the full Plan.

The Python interpreter must be available on the controller's fixed runner PATH;
no Python package dependencies are needed. Normal execution is exclusively
through `hfs-lab execute --plan ... --approval ...` after explicit approval.
The runner supplies `HFS_LAB_PLAN`, `HFS_LAB_PLAN_HASH`, and `HFS_LAB_RUN_ID`.
Calling the driver directly does not grant approval. Its `--help` is offline.

The driver preserves the typed Go snapshot's field order and checks its compact
Go-compatible JSON hash, including HTML escaping. It is not a replacement for
Go's Plan validator. The offline integration fixture below checks both complete
initial-state variants against that actual validator.

## Reviewed policy

Policy schema 1 has exactly these keys; there are no post-approval defaults:

| Key | Required value |
|---|---|
| `schema` | `1` |
| `host_address` | Reviewed literal IPv4/IPv6 address of f3 |
| `host_keys`, `guest_keys` | IDs of pinned local known-hosts artifacts |
| `host_kernel` | Exact expected host `uname -r` |
| `host_packages` | `sha256:` plus digest of canonical host package manifest |
| `allowed_users` | Explicit list of allowed `who` login usernames |
| `max_sessions` | Maximum number of login sessions, including duplicate usernames; zero is allowed |
| `command_timeout_seconds` | Integer 1–15, also bounded by the total step deadline |
| `max_load_1m` | Positive integer ceiling for host one-minute load |
| `disk_path` | Reviewed absolute f3 path on the filesystem supplying outer lab storage |
| `disk_reserve_bytes` | Nonnegative host free-space reserve beyond `Plan.budget.disk_bytes` |
| `allocations` | Exactly five entries, keyed by each Plan target name |
| `stopped` | Exactly one offline snapshot attestation per initially stopped target |

The f3 address must be reviewed from current infrastructure inventory; IQ's
host-address leaf records LAN `192.168.1.133` and WireGuard `192.168.2.133` as
historical context. Neither has been contacted for this implementation. SSH
uses the reviewed literal address, `HostKeyAlias=f3`, and `-F /dev/null` for both
direct host access and the guest's pinned ProxyCommand. It never depends on an
unreviewed `Host f3` alias. Authentication uses the operator's normal default
identity/agent; no private keys or agent forwarding are copied into guests.
Additional custom SSH configuration is deliberately unavailable.

An `allocations` entry has exactly these fields, using actual reviewed values:

```json
{
  "config": "/zroot/bhyve/homelabfs-lab/hfs-test0/hfs-test0.conf",
  "config_sha256": "REVIEWED_64_LOWERCASE_HEX_DIGEST",
  "memory_bytes": 2147483648,
  "cpus": 2,
  "os_disk": "/zroot/bhyve/homelabfs-lab/hfs-test0/disk0.img"
}
```

Each native guest must have 2 GiB and two CPUs. Rocky and the development VM
use their actual reviewed allocations; their historical values are not defaults.
Configuration bytes are hash-checked and their literal `memory`, `cpu` and
`disk0_name` assignments parsed without sourcing or evaluating shell code.

For host and FreeBSD guest packages, canonical manifest bytes are the lines
from `pkg query -a '%n-%v'`, sorted lexically in the controller, joined with
newlines and terminated by one newline. For a running Rocky guest the source
is `rpm -qa --qf '%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH}\n'`, canonicalized the
same way. The driver requires nonempty valid output and successful command
exit. `Target.packages` must be `sha256:` plus the canonical digest.
`Target.kernel` is the exact live `uname -r` for running Rocky, or the reviewed
image kernel evidence for a stopped guest. These are distinct evidence sources.

## Positive evidence for stopped guests

The current Go Plan contract requires all three native guests and the
unrelated `freebsd` guest to start stopped. This driver never boots or contacts
them to obtain version evidence. A historical kernel/package record alone
cannot admit a stopped guest.

Every stopped target needs a **previously reviewed offline image inspection**
bound to its exact OS disk and an existing immutable ZFS snapshot. The
attestation is part of the hash-pinned policy. Its exact fields are:

```json
{
  "kind": "offline-inspected-zfs-snapshot-v1",
  "snapshot": "zroot/bhyve/homelabfs-lab/hfs-test0@REVIEWED_SNAPSHOT",
  "guid": "REVIEWED_NUMERIC_GUID",
  "os_disk": "/zroot/bhyve/homelabfs-lab/hfs-test0/disk0.img",
  "kernel": "REVIEWED_IMAGE_KERNEL",
  "packages": "sha256:REVIEWED_64_LOWERCASE_HEX_DIGEST",
  "provenance": "YYYY-MM-DD reviewer and offline inspection evidence reference"
}
```

The offline inspection must establish the image's intended boot kernel and
complete package manifest. The driver does not independently perform that
inspection or prove a stopped guest's running kernel. It verifies that the
attestation matches `Target.kernel`, `Target.packages` and the reviewed OS disk,
then checks the snapshot's live GUID, `written@SNAPSHOT == 0` on its source
dataset, dataset mountpoint and the OS disk's actual `df` filesystem. A missing
snapshot, changed GUID, changed dataset, any written-since bytes or unavailable
proof blocks admission. Whole-dataset equality is intentionally conservative:
unrelated dataset writes may also block. Evidence output explicitly labels
these versions as reviewed offline image observations.

No suitable attestations have been collected by this implementation. If they
are unavailable, stop at the actionable admission refusal and prepare a new
reviewed Plan only after separately authorized evidence preparation. Do not
invent attestations from the historical manifests, boot guests, mount images,
create snapshots, add holds, or change snapshot ownership to make admission
pass. Existing replication snapshots do not establish offline inspection by
themselves. Snapshot retention/ownership remains with its existing operator;
see [Lab replication](Lab-replication.md). An atime update or other metadata
change can invalidate the deliberately strict zero-written prerequisite.

## Admission and failure behavior

The driver inspects complete current `vm list` output, including `AUTO` values
such as `Yes [1]`, and requires the five reviewed targets' exact initial states
and allocations. Unknown/transitional states, missing/duplicate targets or any
additional running VM block admission. Additional stopped VMs are recorded and
left untouched. Initially running Rocky alone gets pinned guest SSH kernel
and package reads; stopped native guests remain stopped.

Host inspection checks allowed login sessions, physical RAM, free/inactive
pages, the free-page pressure target, load, zero used swap, unchanged swap
page-in/page-out counters across inspection, host package/kernel versions and
outer storage space. The disk parser accepts FreeBSD 15.1's exact
`Filesystem 1024-blocks Used Avail Capacity Mounted on` header. All unavailable
or unparseable evidence blocks.
`swapinfo -k` must show zero used swap, preserving the existing lifecycle
helper's stricter admission rule even if occupied swap is currently inactive.

When Rocky starts stopped, current free+inactive bytes must already cover the
Plan's 6 GiB guest memory and at least 4 GiB host headroom. When Rocky starts
running, preflight checks physical capacity, exact Rocky-only occupancy and
current non-pressure. It does **not** count Rocky's allocation as released
memory. After the approved Rocky stop, the pinned `vms.sh start-lab` also
requires positive current `(free + inactive) * page_size >= 10 GiB` before any
lab VM starts. Insufficient actual memory then refuses startup. Physical RAM,
zero-swap, other-VM and guest-configuration checks remain in place. Because
this fixed helper guards exactly 6+4 GiB, an initially running Rocky Plan must
use exactly 4 GiB host headroom. Larger requested headroom blocks until a
supported helper can enforce it after stop; initially stopped Rocky supports
larger budgets through its actual current-memory check.

The driver issues only fixed read-only inspection commands. It performs no VM
lifecycle, package installation, mount, workload I/O, ZFS mutation, SSH setup or
persistent report writes. SSH may produce normal system authentication logs;
commands do not request state changes. It uses strict pinned host checking,
connect/keepalive limits, a 256 KiB per-command output cap and an overall
budget that ends before the runner's step deadline. Owned local SSH processes
are reaped on timeout, output overflow and handled interruption. There are no
retries. Reaping local SSH does not prove its remote shell or inspection
processes exited. SSH exit 255, local SSH signal termination, in-flight I/O
failure, timeout, output overflow or handled interruption therefore report
`status: "uncertain"` and exit **1**. This deliberately unrecognized nonzero
preserves the existing runner's uncertainty and operator-reconciliation gate;
it must not permit automatic restoration while remote inspections may survive.
Exit 20 is reserved for pre-contact validation failures and confirmed terminal
remote-command failures or admission refusals. Reports go to stdout/stderr for
the runner log.

Admission is a point-in-time check and requires an exclusive reviewed lifecycle
window without competing administrators/autostarts. Login policy checks cover
`who` sessions, not a general process/resource scheduler. Host free space does
not certify capacity inside stopped guest pools; approved native setup must
check that before writing. No native test result, storage mutation guarantee,
future kernel boot result or filesystem correctness is claimed here.

## Offline verification

The focused tests create temporary local fixtures, inject fake inspection
responses and exercise the actual parsers/comparisons. They also extract the
real lifecycle shell body and run it locally with temporary fake `vm`,
`sysctl`, `swapinfo` and `sysrc` commands. That harness never invokes SSH or a
real machine operation. Transport ownership tests substitute local Python
children for SSH and verify exit failure, output limits, timeout and reaping.
Tests pass those outcomes through `main` and check exit 1 for uncertain
transport loss versus exit 20 for confirmed terminal failures. The retained
FreeBSD disk output exercises both parsing and a complete fake admission.

```sh
python3 -B -m unittest discover -s tools/lab -p test_prepared_lab_preflight.py -v
```

To include canonicalization through an **already built** real runner, supply
its absolute path; this test does not build anything or execute a Plan:

```sh
HFS_PREFLIGHT_LAB_BINARY=/absolute/path/to/hfs-lab \
  python3 -B -m unittest discover -s tools/lab \
  -p test_prepared_lab_preflight.py -v
```

The fixture contains all five targets, complete artifact checksums, bounded
run-owned resources, seeds, budgets, test drivers and paired lifecycle steps.
Both initially stopped and initially running Rocky Plans pass the current Go
validator without changing its rules. The realistic running-Rocky fixture has
16 GiB physical RAM and roughly 1.6 GiB currently free/inactive; actual post-stop
memory admission is tested separately with the real shell body. Fixture driver
artifacts are explicitly non-executed placeholders, never native success proof.
