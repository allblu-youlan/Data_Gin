#!/usr/bin/python3
"""Contain the root VS Code process tree on this cgroup v1 host.

Existing anonymous memory charges migrate with tasks. Existing file-cache
charges may stay in the original group. Docker daemon workloads need their
own resource budgets. Use --dry-run before installation and --release only
after stopping the scanner. Releasing limits does not terminate IDE tasks.
"""
import argparse
import errno
import json
import os
from pathlib import Path
import time

GROUP = "vscode-guard"
ROOT = Path("/sys/fs/cgroup")
CONTROLLERS = ("cpu", "blkio", "memory")
MIB = 1024 * 1024


def write_control(controller, name, value):
    (ROOT / controller / GROUP / name).write_text(str(value) + "\n")


def processes():
    result = {}
    host_namespace = os.readlink("/proc/1/ns/pid")
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            if os.readlink(str(entry / "ns/pid")) != host_namespace:
                continue
            groups = (entry / "cgroup").read_text()
            if "docker" in groups or "kubepods" in groups:
                continue
            data = (entry / "stat").read_text()
            fields = data[data.rfind(")") + 2:].split()
            result[int(entry.name)] = {
                "parent": int(fields[1]), "start": fields[19],
                "exe": os.readlink(str(entry / "exe")), "groups": groups,
            }
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
    return result


def select_tree(snapshot, server_root):
    selected = {pid for pid, row in snapshot.items()
                if row["exe"].startswith(server_root)}
    while True:
        descendants = {pid for pid, row in snapshot.items()
                       if row["parent"] in selected}
        expanded = selected | descendants
        if expanded == selected:
            return selected
        selected = expanded


def attach(snapshot, selected):
    for pid in sorted(selected):
        row = snapshot[pid]
        memberships = {}
        for line in row["groups"].splitlines():
            _, controllers, group = line.split(":", 2)
            for controller in controllers.split(","):
                memberships[controller] = group
        for controller in CONTROLLERS:
            if memberships.get(controller) == "/" + GROUP:
                continue
            try:
                data = Path("/proc/{}/stat".format(pid)).read_text()
                if data[data.rfind(")") + 2:].split()[19] != row["start"]:
                    break
                write_control(controller, "cgroup.procs", pid)
            except OSError as error:
                if error.errno in (errno.ESRCH, errno.ENOENT):
                    break
                print("WARNING: retry migration pid={} controller={} error={}".format(
                    pid, controller, error), flush=True)
        if memberships.get("memory") != "/" + GROUP:
            print("migration checked pid={} start={} exe={}".format(
                pid, row["start"], row["exe"]), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="/etc/vscode-resource-guard.json")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--release", action="store_true")
    args = parser.parse_args()
    config = json.loads(Path(args.config).read_text())
    if args.dry_run:
        snapshot = processes()
        for pid in sorted(select_tree(snapshot, config["server_root"])):
            print(pid, snapshot[pid]["parent"], snapshot[pid]["exe"])
        return
    device = os.stat(config["read_device"]).st_rdev
    device_id = "{}:{}".format(os.major(device), os.minor(device))
    if args.release:
        write_control("memory", "memory.memsw.limit_in_bytes", -1)
        write_control("memory", "memory.limit_in_bytes", -1)
        write_control("cpu", "cpu.cfs_quota_us", -1)
        write_control("blkio", "blkio.throttle.read_bps_device", device_id + " 0")
        print("Released limits without terminating processes")
        return
    for controller in CONTROLLERS:
        (ROOT / controller / GROUP).mkdir(exist_ok=True)
    write_control("cpu", "cpu.cfs_period_us", 100000)
    write_control("cpu", "cpu.cfs_quota_us", config["cpu_percent"] * 1000)
    write_control("blkio", "blkio.throttle.read_bps_device",
                  device_id + " " + str(config["read_mib_per_second"] * MIB))
    write_control("memory", "memory.move_charge_at_immigrate", 1)
    snapshot = processes()
    attach(snapshot, select_tree(snapshot, config["server_root"]))
    usage = int((ROOT / "memory" / GROUP / "memory.usage_in_bytes").read_text())
    memsw_usage = int((ROOT / "memory" / GROUP / "memory.memsw.usage_in_bytes").read_text())
    memory_limit = config["memory_mib"] * MIB
    memsw_limit = config["memsw_mib"] * MIB
    active_memory = int((ROOT / "memory" / GROUP / "memory.limit_in_bytes").read_text())
    active_memsw = int((ROOT / "memory" / GROUP / "memory.memsw.limit_in_bytes").read_text())
    already_configured = active_memory == memory_limit and active_memsw == memsw_limit
    over_budget = usage + 256 * MIB > memory_limit or memsw_usage + 256 * MIB > memsw_limit
    if not already_configured and over_budget:
        raise RuntimeError("Memory budget needs review: resident={} MiB, resident+swap={} MiB".format(
            usage // MIB, memsw_usage // MIB))
    write_control("memory", "memory.limit_in_bytes", memory_limit)
    write_control("memory", "memory.memsw.limit_in_bytes", memsw_limit)
    for name, expected in (("memory.limit_in_bytes", memory_limit),
                           ("memory.memsw.limit_in_bytes", memsw_limit)):
        actual = int((ROOT / "memory" / GROUP / name).read_text())
        if actual != expected:
            raise RuntimeError("Memory protection partially configured: " + name)
    print("Active: CPU={}%, memory={}MiB, mem+swap={}MiB, read={}MiB/s; charge={}MiB".format(
        config["cpu_percent"], config["memory_mib"], config["memsw_mib"],
        config["read_mib_per_second"], usage // MIB), flush=True)
    while True:
        time.sleep(config["scan_seconds"])
        snapshot = processes()
        attach(snapshot, select_tree(snapshot, config["server_root"]))


if __name__ == "__main__":
    main()
