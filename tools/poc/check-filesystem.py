#!/usr/bin/env python3
"""Destructive only inside a fresh temporary directory beneath the supplied mount."""
import fcntl
import json
import mmap
import multiprocessing as mp
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tempfile

root = Path(tempfile.mkdtemp(prefix='homelabfs-check-', dir=sys.argv[1]))
results = []

def check(name, fn):
    try:
        detail = fn()
        results.append(dict(test=name, passed=True, detail=detail))
    except Exception as exc:
        results.append(dict(test=name, passed=False, error=repr(exc)))

def basic():
    p = root / 'basic'
    with p.open('wb') as f:
        f.write(b'hello'); f.flush(); os.fsync(f.fileno())
    assert p.read_bytes() == b'hello'
    p.rename(root / 'renamed')
    os.link(root / 'renamed', root / 'linked')
    assert os.stat(root / 'renamed').st_ino == os.stat(root / 'linked').st_ino
    with (root / 'linked').open('r+b') as f:
        (root / 'linked').unlink(); f.seek(0); assert f.read() == b'hello'
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)

def mapped():
    p = root / 'mapped'
    with p.open('w+b') as f:
        f.truncate(4096)
        with mmap.mmap(f.fileno(), 4096) as mm:
            mm[:4] = b'test'; mm.flush()
        os.fsync(f.fileno())
    assert p.read_bytes()[:4] == b'test'

def denied_child(path, conn):
    try:
        os.setgroups([]); os.setgid(60002); os.setuid(60002)
        try:
            with open(path, 'rb') as f: f.read()
            conn.send(False)
        except PermissionError: conn.send(True)
    except Exception as exc: conn.send(repr(exc))
    finally: conn.close()

def permissions():
    os.chmod(root, 0o755)
    p = root / 'private'; p.write_bytes(b'secret')
    os.chown(p, 60001, 60001); os.chmod(p, 0o600)
    st = p.stat(); assert (st.st_uid, st.st_gid, st.st_mode & 0o777) == (60001,60001,0o600)
    a,b=mp.Pipe(); child=mp.Process(target=denied_child,args=(str(p),b)); child.start()
    child.join(5)
    if child.is_alive(): child.terminate(); child.join(); raise AssertionError('permission test timeout')
    assert a.poll(1) and a.recv() is True

def contender(path, kind, conn):
    try:
        # Separate process opens its own file description.
        with open(path, 'r+b') as f:
            try:
                (fcntl.flock if kind == 'flock' else fcntl.lockf)(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
                conn.send(False)
            except BlockingIOError: conn.send(True)
    except Exception as exc: conn.send(repr(exc))
    finally: conn.close()

def locking(kind):
    p=root / kind; p.write_bytes(b'lock')
    with p.open('r+b') as f:
        (fcntl.flock if kind == 'flock' else fcntl.lockf)(f, fcntl.LOCK_EX)
        a,b=mp.Pipe(); child=mp.Process(target=contender,args=(str(p),kind,b)); child.start(); child.join(5)
        if child.is_alive(): child.terminate(); child.join(); raise AssertionError('lock test timeout')
        assert a.poll(1) and a.recv() is True, 'competing process obtained lock or errored'

def database(mode):
    p=root / ('db-'+mode)
    c=sqlite3.connect(p)
    try:
        assert c.execute('pragma journal_mode='+mode).fetchone()[0].lower() == mode.lower()
        c.execute('pragma synchronous=FULL'); c.execute('create table t (n integer primary key, v text)')
        for i in range(20): c.execute('insert into t values (?,?)',(i,'payload')); c.commit()
        assert c.execute('pragma integrity_check').fetchone()[0] == 'ok'
    finally: c.close()
    c=sqlite3.connect(p)
    try: assert c.execute('select count(*) from t').fetchone()[0] == 20
    finally: c.close()

def database_crash():
    p = root / 'crash.sqlite'
    code = """import os, sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute('pragma journal_mode=WAL')
c.execute('pragma synchronous=FULL')
c.execute('create table t(n integer)')
c.execute('insert into t values(1)')
c.commit()
c.execute('insert into t values(2)')
os._exit(0)
"""
    subprocess.run([sys.executable, '-c', code, str(p)], check=True, timeout=15)
    c = sqlite3.connect(p)
    try:
        assert c.execute('select * from t').fetchall() == [(1,)]
        assert c.execute('pragma integrity_check').fetchone()[0] == 'ok'
    finally:
        c.close()

try:
    check('write-fsync-rename-hardlink-unlink-open-directory-fsync',basic)
    check('uid-gid-mode-and-denied-read',permissions)
    check('mmap-flush-fsync',mapped)
    check('flock-contention',lambda:locking('flock'))
    check('fcntl-contention',lambda:locking('fcntl'))
    check('sqlite-delete-full-sync',lambda:database('DELETE'))
    check('sqlite-wal-full-sync',lambda:database('WAL'))
    check('sqlite-wal-process-crash',database_crash)
finally:
    shutil.rmtree(root)
print(json.dumps(results,indent=2))
sys.exit(0 if all(r['passed'] for r in results) else 1)
