# Plan-bound boot and restoration readiness

`tools/lab/wait-lab-ready.py` is a read-only driver of the existing
[lab runner](Lab-runner.md). It has local subprocess fixture validation only;
implementation does not establish native or Rocky runtime readiness. Running it
against the lab still requires fresh approval of the complete concrete Plan.

Pin this executable as an ordinary Plan artifact. It reuses the first
`prepared-lab-preflight-v1` step's pinned policy and its `host_address`,
`host_keys` and `guest_keys` artifact IDs. Paths come from those exact Plan
artifacts. Host SSH uses the reviewed literal address and `HostKeyAlias=f3`;
guest SSH uses pinned root access through that host. Native keys must equal
`docs/lab/f3-known_hosts`; Rocky restoration requires its reviewed guest key.
No keys are discovered or accepted automatically.

Declare one of these exact steps, using your policy artifact ID in place of
`preflight-policy`:

| Mode | Phase | Contract | Args |
|---|---|---|---|
| Native boot | `setup` | `lab-readiness-v1:native-boot` | `["--mode", "native-boot", "--policy-artifact", "preflight-policy"]` |
| Rocky restoration | `restore` | `lab-readiness-v1:rocky-restore` | `["--mode", "rocky-restore", "--policy-artifact", "preflight-policy"]` |

Both use `action: "driver"`, `effects: "read-only"`, `max_attempts: 1` and a
step timeout of at least 180 seconds. Place native readiness after `start-lab`
and before native setup. Place Rocky readiness after the approved `start-rocky`
restoration action, with the same applicable forward-step dependencies. It
requires initially running Rocky in the Plan. VM lifecycle remains the runner's
responsibility; a successful VM start alone cannot establish SSH restoration.

The driver polls completed host `vm list` output before guest checks. Every
response must contain exactly all five targets with well-formed states and no
duplicate rows. Only the guests being started may remain `Stopped` or show
`Bootloader (PID)` during this bounded wait; unknown states or malformed PIDs
fail immediately. Every excluded VM must be `Stopped` on every inventory poll,
including while expected guests are booting. No guest TCP or SSH probe starts
until all three native guests are `Running (PID)` for native boot, or Rocky is
`Running (PID)` for restoration. The final inventory after guest checks must
still show that exact running/stopped arrangement; it cannot resume boot polling.
Each selected guest must return its exact Plan kernel and UID 0 over pinned
SSH. Evidence is printed into the runner log; no separate evidence format or
persistent driver state is introduced.

The driver bounds each command to eight seconds and 16 KiB of combined output,
with 170 seconds total shared by boot-inventory polling and guest checks. Only
the completed expected boot states above or completed TCP probe exit 1 with
empty stdout permit another probe. SSH exit 255, local signal termination,
timeout, overflow or interruption returns uncertain exit 1 immediately. Local
SSH children are killed/reaped when
necessary; this never proves remote completion. Confirmed terminal refusals use
exit 20. Handled interruption is deferred across child creation and ownership
handoff; the child restores the original signal mask before exec. Local fixtures
cover both signals arriving after child startup but before the spawn call returns,
including driver reaping, no retry and parent/child signal-mask restoration.
Routine checks are local: `python3 -B -m unittest discover -s tools/lab
-p 'test_wait_lab_ready.py'`; their fake SSH children never contact a host.
