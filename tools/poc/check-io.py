#!/usr/bin/env python3
"""Bounded I/O smoke benchmark; uses only a fresh temporary subdirectory."""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import random
import shutil
import sys
import tempfile
import time

root = Path(tempfile.mkdtemp(prefix='homelabfs-io-', dir=sys.argv[1]))
results = {}

def sequential(path, size):
    chunk = 256 * 1024
    expected = hashlib.sha256()
    start = time.monotonic()
    with path.open('wb', buffering=0) as f:
        for _ in range(size // chunk):
            data = os.urandom(chunk)
            expected.update(data)
            assert f.write(data) == len(data)
        os.fsync(f.fileno())
    write_seconds = time.monotonic() - start
    actual = hashlib.sha256()
    start = time.monotonic()
    with path.open('rb', buffering=0) as f:
        while data := f.read(chunk):
            actual.update(data)
    read_seconds = time.monotonic() - start
    assert actual.digest() == expected.digest()
    return dict(bytes=size, sha256=actual.hexdigest(), verified=True,
                write_MiB_s=round(size / 1048576 / write_seconds, 2),
                read_MiB_s=round(size / 1048576 / read_seconds, 2),
                write_seconds=round(write_seconds, 3), read_seconds=round(read_seconds, 3))

try:
    results['sequential'] = sequential(root / 'sequential', 128 * 1024 * 1024)
    rng = random.Random(42)
    expected = {}
    latencies = []
    fd = os.open(root / 'random', os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
    try:
        os.ftruncate(fd, 16 * 1024 * 1024)
        for _ in range(256):
            offset = rng.randrange(4096) * 4096
            data = os.urandom(4096)
            start = time.monotonic()
            assert os.pwrite(fd, data, offset) == 4096
            os.fsync(fd)
            latencies.append((time.monotonic() - start) * 1000)
            expected[offset] = data
    finally:
        os.close(fd)
    fd = os.open(root / 'random', os.O_RDONLY)
    try:
        for offset, data in expected.items():
            assert os.pread(fd, 4096, offset) == data
    finally:
        os.close(fd)
    latencies.sort()
    results['random_4KiB_write_fsync'] = dict(operations=256, verified_blocks=len(expected),
        p50_ms=round(latencies[128], 3), p95_ms=round(latencies[243], 3),
        max_ms=round(latencies[-1], 3), verified=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        futures = [pool.submit(sequential, root / ('parallel-'+str(i)), 16 * 1024 * 1024) for i in range(4)]
        results['four_parallel_files'] = [f.result() for f in futures]
finally:
    shutil.rmtree(root)
print(json.dumps(results, indent=2))
