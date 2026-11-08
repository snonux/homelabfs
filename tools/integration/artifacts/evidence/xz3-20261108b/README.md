# xz3 f44 rebuild (2026-11-08): MKDIR-targeted lost reply

This candidate supersedes the [earlier 2026-11-08 candidate](../xz3-20261108/README.md)
(`3f770a88…`, source `e0cca0c`) only because task d44 changed runtime code: the
v24 lost-reply fixture can now be armed on the MKDIR CALL alone (ARM code 2),
which zq3 T9 needs. **Build and offline admission only:** nothing was installed
or booted, and `installed`, `booted` and `native_accepted` stay false with
publication null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261108b/package` |
| Manifest SHA256 | `b203799e3f631888b2c2edc24e1d38b913249bea978e0d4d954886f589d04fa2` (194 files) |
| Source checkout | `defd974af3721614b1a52d24fd949822ef5cd735` |
| CGO daemon | `174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7` |
| Window test binary / helper | `9e046c20…` / `a773f3ef…` (from `defd974`) |

[Evidence](evidence.json) records hashes, receipts and control names.

## What was rebuilt and what was reused

- **Reused byte for byte (136 files):** the 83 native FreeBSD files, the source
  bundle and its notices, and the Linux kernel, modules, config, clean-repro
  and ABI records with their inputs and licenses. `git diff e0cca0c defd974`
  is empty for `tools/integration/lib9p`, `tools/integration/linux9p` and every
  kernel and native recipe. Each file was checked against the earlier manifest
  and the native first seal before reuse.
- **Rebuilt:** the CGO daemon, from `defd974`; two forced builds produced
  identical bytes.
- **Window artifacts:** rebuilt from `defd974` so the f3 host qualification
  test bytes match the daemon source. The helper's hash differs only because
  its debug info records the new build directory; its C source is unchanged.
  The same recipe reproduced the new daemon exactly.
- **G3 init helper:** kept at `8c02b98f…` from `e0cca0c`. The non-test Go
  changes up to `defd974` are limited to the lost-reply fixture, the lifecycle
  client and the new prep packages, and no volume-format code changed.

## Checks

`assemble.py` sealed and admitted the package, and an independent `admit.py`
run passed. `check_candidate.py` refused all 26 real-copy mutations, including
the real CGO0 daemon, and the original and restored copies passed again. On f3
the exact daemon bytes passed `--check-process-observer` under the bounded host
guard (Rocky pinned). `--check-descriptor-exec` refused as expected, because
the f3 host `/dev/fd` is still devfs. Enabling fdescfs there is part of the zq3
window and needs explicit approval.

The tooling now admits only this manifest. The earlier one is refused as
superseded, and `7e962627…` stays quarantined.
