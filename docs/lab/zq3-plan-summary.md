# zq3 run plan in plain language

This is the short version of the [full zq3 run plan](zq3-run-plan.md). It is a
proposal. Nothing has been done on f3 or Rocky yet, and nothing will be until
you say yes to a filled-in version of it.

## What the test is for

Until now, the Rocky test VM reached files on f3 through a throwaway test
program. This run replaces that program with the real HomelabFS daemon, still
on one machine and only with throwaway test data. We want to know whether
normal Linux file behaviour works through that path: file owners and groups,
"save to disk" (sync), memory-mapped files, file locks, what happens when the
daemon restarts, and whether a program can accidentally write into an empty
folder when the real mount is gone (the K04 check).

## What happens to f3

- New test files are copied into a private folder under `/var/tmp`. Nothing
  in the normal system is replaced, including the normal `bhyve` program.
- Two FreeBSD kernel modules are loaded: `fdescfs` (needed so the daemon can
  start helper programs safely) and `fusefs` (the filesystem plumbing).
  **They stay loaded afterwards** until f3's next reboot. They are not unloaded,
  because unloading is riskier than leaving them.
- A special folder, `/dev/fd`, gets a temporary mount for the duration of the
  test, and is put back exactly as it was at the end.
- One temporary network port (`tap`) is added to f3's VM network for the test
  copy of Rocky, then removed.
- A throwaway SSH key is created on f3 just for this run and deleted afterwards.

## What happens to Rocky

1. **While Rocky is still running normally**, a test Linux kernel is copied
   onto its disk. It is not selected yet; Rocky's normal kernel stays the
   default.
2. The throwaway SSH key from f3 is allowed into Rocky's root account with
   tight restrictions (it can only run one read-only check). Your own keys are
   not touched and are never copied anywhere.
3. Right before the shutdown, the test kernel is marked "use once on the next
   boot only", and Rocky is shut down cleanly (no forced power-off).
4. The HomelabFS daemon on f3 starts Rocky itself, with 6 GiB of memory instead
   of the usual 12 GiB, booting the test kernel once.
5. The tests run inside Rocky, only in new test folders.
6. Rocky is shut down cleanly again, the extra SSH key line is removed, and
   Rocky is started the normal way (12 GiB, normal kernel).
7. The test kernel is removed again (unless you want to keep it for reruns).

## How long Rocky is down

Normally **about 45 to 60 minutes**, counted from the shutdown; our planning
estimate allows up to 75 minutes. These are estimates, not a timer that
enforces them. If the test stops early (after the shutdown but before the test
copy boots), add about 10 minutes for one extra boot (see below). If something
goes wrong in an unclear way, Rocky stays off until someone has made sure the
test copy is really gone, because starting the normal Rocky while the test copy
might still run could damage its disk. That has no fixed time limit.

## What could go wrong

- **Memory is tight.** The test needs about 15 of f3's 15.7 GiB. The plan
  measures free memory after Rocky stops and refuses to start if it does not fit.
- **The "use once" mark is used up by the first boot, whichever that is.** If
  the test stops after Rocky was shut down but before the test copy booted, the
  mark is still there, and Rocky's next normal start would boot the test
  kernel. We would then let it boot the test kernel once, check that the mark
  is gone, and reboot it cleanly into the normal kernel (about 10 extra
  minutes). Alternatively someone picks the normal kernel by hand in Rocky's
  boot menu on the VNC console. If the test stops before Rocky is shut down,
  the mark is simply removed again.
- **The test kernel might not boot or might hang.** A hung Rocky cannot shut
  itself down cleanly, and the only way out is a forced power-off. We would
  never do that on our own; we would stop and ask you first.
- **Rocky's disk is shared** between the normal setup and the test copy. Nobody
  may start the normal Rocky while the test runs.
- **The daemon restart test may end the run.** If the restart is refused, the
  daemon stops on purpose (you accepted this behaviour). The test copy of Rocky
  is then shut down from inside.
- **An old setting** in Rocky's normal configuration still points to a folder
  from the earlier test (`/var/tmp/homelabfs-9p-poc/share` on f3). It was
  checked on 2026-11-08 and exists, so the normal Rocky start works; the plan
  re-checks it before stopping Rocky.

Nothing is force-killed without asking you, nothing is force-unmounted, and
failed test leftovers are kept for inspection instead of being deleted.

## How it is undone

Clean shutdown of the test copy, removal of the temporary network port and
`/dev/fd` mount, removal of the SSH key line, a check that the "use once" mark
is used up, normal start of Rocky with its normal kernel, then removal of the
test kernel and test files. (After an early stop, the extra test-kernel boot
and clean reboot described above come first.) Only the two loaded modules on
f3 remain until f3 reboots.

## What is ready now (2026-11-08)

All the helper scripts the plan needs now exist, each reviewed and tested on
the workstation only: starting Rocky under the daemon (startup orchestrator),
the configuration generator, the volume setup helper, the test-copy launch and
cleanup script, the kernel install/removal script with its installed-kernel
record, the SSH key script, the tool that checks the daemon's records against
what Rocky did, the pod test, the lost-reply and restart steps, the host-side
"empty folder" check, the `/dev/fd` plan with rebuilt test programs, and a
memory/time watchdog for the daemon. The test bundle was rebuilt so it contains
all of this, and its daemon passed a read-only self-check on f3. On 2026-11-09
its guest kernel was rebuilt once more (`5eb42c50…`) to fix the mount error
that stopped the second attempt (zq3-r2), and then again (`89c928d5…`) so a
failed mount no longer crashes the guest kernel, as it did in the third
attempt (zq3-r3). Then the daemon was rebuilt (`2e0412a6…`, daemon
`39c92d01…`) so it answers every guest view poll, including one that arrives
before the daemon has taken the domain, which hung the fourth attempt
(zq3-r4); the guest kernel is unchanged. These daemon bytes passed the f3
self-check in the fifth attempt (zq3-r5). On 2026-11-10 the guest kernel was
rebuilt once more (`e01fab21…`) so two guest requests that share one fixed
message slot no longer collide and stop the mount, as they did in the fifth
attempt (zq3-r5); the daemon is unchanged. Later that day both were rebuilt
together (`66b5bb2b…`): the guest kernel now really starts SELinux (earlier
test kernels had it built in but never switched on), and the daemon (`9b7eed8d…`)
keeps a separate view counter per domain and accepts a guest message that
arrives slightly out of order, the two faults of the sixth attempt (zq3-r6).
The new daemon must pass the f3 self-check again.

Still true: nothing has been tested on the real f3 or Rocky; the page-table
pinning checks cannot run at all yet; and the run is driven step by step with a
written record of every step, not by a fully automatic runner. Right before the
run, fresh read-only facts are collected and the exact file hashes are filled
into the plan; you then get the final plan to approve.

## The decisions you need to make

Please answer each with yes or no:

1. **Rocky downtime:** may Rocky be stopped for about an hour (estimated up to
   75 minutes from the shutdown, not a hard limit; longer if something needs
   inspecting)?
2. **Test files on f3:** may the test programs be copied into a private
   `/var/tmp` folder on f3?
3. **Test kernel on Rocky:** may a test kernel be installed on Rocky's disk and
   selected for a one-time boot right before the shutdown, with the normal
   kernel kept as the default? This includes the recovery after an early stop:
   one boot of the test kernel under Rocky's normal settings, then a clean
   reboot into the normal kernel. (Or would you rather pick the normal kernel
   by hand on the VNC console in that case?)
4. **Remove afterwards:** should the test kernel be removed after the run (yes)
   or kept for future reruns (no)?
5. **Rocky under the daemon:** may the daemon start Rocky itself once, with
   6 GiB and a temporary network port, instead of the usual way?
6. **f3 kernel modules:** may `fdescfs` and `fusefs` be loaded on f3 and stay
   loaded until f3's next reboot, with a temporary `/dev/fd` mount during the run?
7. **Throwaway SSH key:** may a run-only key be created on f3 and allowed into
   Rocky's root account (restricted, removed afterwards)?
8. **Signing key:** the daemon needs signatures from the operator key
   (`2f6d909c…`). Do you want to run the signing step yourself, or allow the
   agent to use that key file during the run (tell us where it is)?
9. **Signing twice within two minutes:** during startup the daemon waits at
   most 120 seconds, and the operator key has to sign twice in that time (the
   commands are printed and can be prepared in advance). Is that workable for
   you, or should the agent sign with the key file you point it to (question 8)?
10. **Root helper on f3:** the startup script runs its f3 steps through a
    `doas` rule for `python3.12 -I -B -c`, which in practice means unrestricted
    root for your user on f3 during the run. May that rule be added for the run
    (and removed afterwards)?

Already settled by your earlier "defaults are fine": the pod test uses the
installed `podman`/`crun` with a container built only from Rocky's own files
(no downloads), and the w24 guest test counts as a normal lab action.
