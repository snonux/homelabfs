# Plan-bound boot and restoration readiness

`tools/lab/wait-lab-ready.py` is a read-only driver of the existing
[lab runner](Lab-runner.md). Its local socket/subprocess fixture validation
does not establish native or Rocky runtime readiness. Running it
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

Before its first host SSH command in either mode, the controller waits for a
local TCP connection to the policy's reviewed literal `host_address` on port 22.
It uses that IP directly, without DNS or a remote command. Only completed
connect refusal, timeout, host-unreachable or network-unreachable results may
wait and retry. Unexpected socket errors fail immediately, and interruption
remains uncertain. Each socket closes before another probe or SSH dispatch.
TCP readiness establishes only connectivity: the original pinned SSH inventory
and identity checks remain mandatory. Once SSH is dispatched, no SSH failure
returns to this gate or receives a retry based on its error text.

The driver then polls completed host `vm list` output before guest checks. Every
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

The driver bounds each local TCP connect and SSH command to eight seconds;
SSH output has a 16 KiB combined limit. One original 170-second deadline is
shared by host TCP readiness, boot-inventory polling and guest checks. After
host TCP readiness, only the completed expected boot states above or completed
guest TCP probe exit 1 with empty stdout permit another probe.
SSH exit 255, local signal termination,
timeout, overflow or interruption returns uncertain exit 1 immediately. Local
SSH children are killed/reaped when
necessary; this never proves remote completion. Confirmed terminal refusals use
exit 20. Handled interruption is deferred across child creation and ownership
handoff; the child restores the original signal mask before exec. Local fixtures
cover both signals arriving after child startup but before the spawn call returns,
including driver reaping, no retry and parent/child signal-mask restoration.
Routine checks are local: `python3 -B -m unittest discover -s tools/lab
-p 'test_wait_lab_ready.py'`. Fixtures redirect the asserted reviewed host IP
and port to real loopback sockets, covering refusal then readiness and a bounded
connect timeout against a full local accept queue. They also cover deadline
exhaustion, unexpected network errors, interruption before SSH, and terminal SSH
uncertainty after TCP readiness. Their fake SSH children never contact a host.

The runner log retains JSON `ssh-command` records on driver stdout for uncertain
commands, completed nonzero SSH exits, and successful commands with diagnostic
stderr. Records include the reviewed argv, observed numeric `returncode`, and
separate stdout/stderr excerpts sharing the existing 16 KiB collection cap.
`output_truncated: true` marks overflow; overflow still stops immediately as
uncertain. A null return code means no original SSH exit was observed before
local cleanup. A negative observed code records signal termination; a later
cleanup SIGKILL is not reported as the original outcome or proof of remote
completion. Uncertain records also include the reason for stopping. The terminal
status/error JSON remains on driver stderr.

Only the fixed read-only command argv and public host-key file paths are logged;
the driver does not dump environment variables or read private-key contents.
JSON escapes terminal controls, and invalid UTF-8 is replaced in log excerpts
only. Exact command stdout bytes still supply inventory, TCP and identity
parsing; warnings on stdout are never filtered away, and diagnostic stderr does
not affect those checks. Local fixtures cover actual child streams, successful
warnings, completed nonzero exits, exit 255, signal termination, timeout, and
the combined output boundary/overflow. These added diagnostics cannot recover
the discarded streams or establish the original n4 restoration failure's cause.
