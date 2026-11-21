#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-only
"""Run the actual hfs_setattr and hfs_dentry_attach against the VFS callers (vf4).

zq3-r18 R18-1: every chown/fchown of a regular file carries ATTR_KILL_* bits
(chown_common, fs/open.c), and hfs_setattr refused them with EOPNOTSUPP before
any RPC. The production setattr functions (kill-bit handling, support check,
both argument writers, hfs_setattr itself), the dentry attach helper with
hfs_dops and its callbacks, hfs_dcap_write, hfs_cap_write, hfs_cap_free and
hfs_args_init are copied verbatim from the owned source tree, together with
the ATTR_* values of include/linux/fs.h. The JSON buffer and parser are the
real hfs_protocol.c. test.c models the VFS callers (chown_common, do_truncate,
__remove_privs and notify_change's kill-bit conversion, fs/attr.c) and
substitutes setattr_prepare, hfs_begin/hfs_end, hfs_call and d_set_d_op (which
fails the test where the kernel would WARN). No kernel, VM or service is used.

G2: the source check below also requires that hfs_inode.c calls d_set_d_op
only inside hfs_dentry_attach, and that hfs_lookup, hfs_create_install (CREATE)
and hfs_namespace_install (MKDIR/SYMLINK) attach through it.

Review 1: hfs_lookup, hfs_relookup, hfs_link and hfs_create_exists are copied
as well and run on a dentry an earlier hfs_lookup already hashed;
d_splice_alias's BUG_ON(!d_unhashed(dentry)) is substituted by a test failure.
The source check requires that hfs_lookup is called only by hfs_atomic_open
(for an in-lookup dentry) and by hfs_relookup, which unhashes first.
"""
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import sys


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def attr_constants(fs_header):
    """The kernel's own ATTR_* bit values, so the model and the code agree."""
    lines = re.findall(r'^#define ATTR_[A-Z_]+\s+\(1 << \d+\)', fs_header, re.M)
    if len(lines) < 17:
        raise ValueError('include/linux/fs.h ATTR_* definitions changed')
    return '\n'.join(lines) + '\n'


def check_attach_callers(inode):
    """G2: d_set_d_op only in hfs_dentry_attach; the three creators use it.

    Review 1: a second hfs_lookup of a hashed dentry goes through
    hfs_relookup (d_drop first)."""
    build_function = load('controlled_received_build',
                          Path(__file__).resolve().parent.parent /
                          'controlled_received/build.py').function
    attach = build_function(inode, 'hfs_dentry_attach')
    if inode.count('d_set_d_op(') != 1 or 'd_set_d_op(' not in attach:
        raise SystemExit('FAIL: d_set_d_op outside hfs_dentry_attach (G2)')
    for name in ('hfs_lookup', 'hfs_create_install', 'hfs_namespace_install'):
        body = build_function(inode, name)
        if 'hfs_dentry_attach(dentry, cap);' not in body or 'dentry->d_fsdata = cap;' in body:
            raise SystemExit(f'FAIL: {name} does not attach through hfs_dentry_attach')
    relookup = build_function(inode, 'hfs_relookup')
    atomic_open = build_function(inode, 'hfs_atomic_open')
    if (inode.count('hfs_lookup(dir, dentry, 0)') != 2 or
            'd_drop(dentry);\n\treturn hfs_lookup(dir, dentry, 0);' not in relookup or
            'if (d_in_lookup(dentry)) {\n\t\tstruct dentry *alias = hfs_lookup(dir, dentry, 0);'
            not in atomic_open):
        raise SystemExit('FAIL: hfs_lookup called outside the in-lookup open and hfs_relookup')


def extract(source, output, build):
    """Write verbatim production definitions and return their source hashes."""
    texts = {name: (source / name).read_text()
             for name in ('fs/9p/hfs.h', 'fs/9p/hfs_session.c', 'fs/9p/hfs_inode.c',
                          'include/linux/fs.h')}
    inode, session = texts['fs/9p/hfs_inode.c'], texts['fs/9p/hfs_session.c']
    header = texts['fs/9p/hfs.h']
    check_attach_callers(inode)
    (output / 'attr.inc').write_text(attr_constants(texts['include/linux/fs.h']))
    (output / 'cap.inc').write_text(header[header.index('struct hfs_cap {'):
                                           header.index('\nstruct hfs_cred {')] + '\n' +
                                    ''.join(build.function(session, name) for name in (
                                        'hfs_args_init', 'hfs_cap_write', 'hfs_cap_free')))
    start = inode.index('static int hfs_d_revalidate(')
    dops = inode[start:inode.index('\n};\n', start) + 4]
    (output / 'dentry.inc').write_text(build.function(inode, 'hfs_dcap_write') + dops +
                                       build.function(inode, 'hfs_dentry_attach'))
    kill = re.search(r'^#define HFS_ATTR_KILL .*$', inode, re.M).group(0)
    (output / 'setattr.inc').write_text(kill + '\n' + ''.join(
        build.function(inode, name) for name in (
            'hfs_time_write', 'hfs_optional_time', 'hfs_setattr_sent',
            'hfs_setattr_supported', 'hfs_truncate_args', 'hfs_setattr_args',
            'hfs_setattr')))
    (output / 'lookup.inc').write_text(''.join(build.function(inode, name) for name in (
        'hfs_lookup', 'hfs_relookup', 'hfs_create_exists', 'hfs_link')))
    return {name: hashlib.sha256(text.encode()).hexdigest() for name, text in texts.items()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--sanitize', action='store_true')
    args = parser.parse_args()
    sys.dont_write_bytecode = True
    here = Path(__file__).resolve().parent
    source, output = args.source.resolve(strict=True), args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    runner = load('codec_runner', here.parent / 'codec/run.py')
    build = load('controlled_received_build', here.parent / 'controlled_received/build.py')
    hashes = extract(source, output, build)
    log = output / 'setattr.log'
    log.write_text(''.join(f'{name}_sha256={value}\n' for name, value in sorted(hashes.items())))
    command = ['cc', '-std=gnu11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
               '-Wno-sign-compare', '-Wno-unused-parameter', '-DHFS_CODEC_USERSPACE',
               '-include', str(here.parent / 'policy/compat.h'),
               '-I', str(source / 'fs/9p'), '-I', str(output),
               str(source / 'fs/9p/hfs_protocol.c'), str(here / 'test.c'),
               '-lcrypto', '-o', str(output / 'setattr-test')]
    if args.sanitize:
        command[1:1] = ['-fsanitize=address,undefined', '-fno-omit-frame-pointer']
    runner.run(command, here, log)
    environment = dict(os.environ)
    environment['ASAN_OPTIONS'] = 'detect_leaks=1:allocator_may_return_null=1:quarantine_size_mb=16'
    runner.run([str(output / 'setattr-test')], here, log, environment)
    print(log.read_text())


if __name__ == '__main__':
    main()
