#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-2-Clause
"""Compute the inert candidate placement; no VM/configuration action exists."""
import argparse
import json
from pathlib import Path

MIB = 1 << 20
GIB = 1 << 30


def proposal(physical_bytes):
    # Service allocation includes the preserved 512+768 MiB ordinary/recovery
    # baseline. C10 uses its actual replay service, not logicalview.New.
    service = 1280 * MIB
    exporter = 274 * MIB
    guest_copy = 64 * MIB
    controller = 256 * MIB
    claims, retained, source = 128, 32 * MIB, 16 * MIB
    domains = 2
    providers = 4 * (retained + source + claims * 4096 + 16 * MIB) * domains
    owners = {'transport_arenas_parser': 652 * MIB,
              'durable_c8_mmap_serializer': GIB,
              'service_including_retained_recovery_baseline': service,
              'exporter_executable_backend_snapshots_management': exporter,
              'guest_copy_owners': guest_copy,
              'controller_source_ledger_work': controller,
              'provider_retained_source_identity_four_copies': providers}
    host = 6 * GIB
    configured_vm = 6 * GIB
    host_reserve = 3 * GIB
    guest = {'base_and_kernel': 512 * MIB,
             'ordinary_request_completion_credits': 128 * MIB,
             'control_request_completion_credits': 128 * MIB,
             'reservation_metadata': 32 * 131072,
             'cookies': 64 * 4096, 'received_metadata': 64 * 4096}
    if sum(owners.values()) > host or sum(guest.values()) > 2 * GIB:
        raise ValueError('unfunded candidate owners')
    total = host + configured_vm + host_reserve
    return {'version': 'homelabfs-artifact-installation-proposal/1',
            'action': 'PROPOSAL_ONLY', 'publication': None,
            'logical_bytes': 4 * MIB, 'page_size': 4096,
            'domains': domains, 'guest_topology': 'same-vm-boot-two-independent-mounts',
            'physical_bytes': physical_bytes,
            'host_runtime_envelope_bytes': host,
            'host_owner_reservations': owners,
            'host_runtime_remaining_for_measured_heap_allocator_kernel_metadata': host - sum(owners.values()),
            'configured_rocky_bytes': configured_vm,
            'guest_signed_ceiling_bytes': 2 * GIB,
            'guest_owner_reservations': guest,
            'guest_remaining_for_measured_kernel_MM_PTE_stack_slab_userspace': 2 * GIB - sum(guest.values()),
            'host_nonruntime_reserve_bytes': host_reserve,
            'configured_total_bytes': total,
            'physical_headroom_bytes': physical_bytes - total,
            'arithmetic_fits': total <= physical_bytes,
            'placement_admitted': False,
            'pending': [
                'Separate exact approved clean shutdown, Rocky RAM change and boot window; all native guests stopped.',
                'Selected guest has 6GiB VM RAM but signed provider/guest workload envelope remains at most2GiB; no native guest allocation increase.',
                'Fresh target hardware/rootfs/security/initramfs and installed/running bytes, full module inventory, measured host/guest allocator/MM/header/library peaks.',
                'Actual full4MiB mapped pressure with ordinary saturation and funded control drains; no SIGBUS-as-success or multiplying every dirty metadata slot by16MiB.',
                'Controller actual source/ticket/claim/work admission and real service replay/recovery peak; preserved logicalview baseline is separate compatibility accounting.',
                'Fresh healthy pool, snapshot/reservation/quota ownership and run/staging storage admission; preserve8GiB unreserved host and2GiB guest disk headroom.',
                'Complete independent domain boot/provider/idmap measurement, external signed provisioning, pending device ownership, all HSM5 claims and funded protected HELLO beforeREADY.',
                'Native FUSE/directIO/private inode, PTE/TLB/old-domain exclusion, lost-reply recovery and external reply oracle acceptance in zq3.',
                'Release signing/publication remains independent later tr3; no normal authority, replacement exporter/FUSE or guest RESUME enabled.'
            ]}



def pinned_proposal(package, physical_bytes, operator):
    """Inert installation envelope bound to already built component bytes."""
    from package import POLICY, PROTOCOL, digest, read_json
    value = proposal(physical_bytes)
    artifacts = {}
    for relative in ('linux/build.json', 'freebsd/build.json', 'freebsd/daemon-build.json'):
        record = read_json(package / relative)
        for name, expected in record['artifacts'].items():
            if digest(package / name)[1] != expected:
                raise ValueError('proposal artifact differs from actual build')
            artifacts[name] = expected
    value.update(operator_public=operator, protocol=PROTOCOL, policy=POLICY,
        artifacts=artifacts, execution_authorized=False,
        target='existing disposable Rocky on f3; native guests stopped',
        staging_prefix='/var/tmp/hfs-validation-xz3',
        host_executables='stage under private prefix; never replace system bhyve',
        kernel_release=(package / 'linux/kernel.release').read_text().strip(),
        boot_policy='separate approved one-shot guest kernel/initramfs/module selection; retain stock boot default and rollback',
        proposed_limits={'seconds': 3600, 'host_run_disk_bytes': 4 * GIB,
                         'guest_run_disk_bytes': 2 * GIB, 'logical_canary_bytes': 4 * MIB},
        admission='fresh exact plan, target hardware/rootfs/security and rollback inventory, external consent/enrollment, installed bytes and live scope required before any execution',
        authority='proposal only; no installation, VM lifecycle, reboot, mount or exporter execution authorized')
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--physical-bytes', type=int, required=True)
    parser.add_argument('--package', type=Path)
    parser.add_argument('--operator-public')
    args = parser.parse_args()
    if args.physical_bytes <= 0:
        raise ValueError('physical memory required')
    if args.package is not None and args.operator_public is None:
        raise ValueError('operator identity required for artifact proposal')
    value = pinned_proposal(args.package.resolve(strict=True), args.physical_bytes, args.operator_public) if args.package is not None else proposal(args.physical_bytes)
    print(json.dumps(value, sort_keys=True, indent=2))


if __name__ == '__main__':
    main()
