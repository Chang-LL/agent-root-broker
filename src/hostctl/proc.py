from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class ProcessIdentity:
    pid: int
    uid: int
    start_time: int

    @property
    def key(self) -> str:
        return f"{self.uid}:{self.pid}:{self.start_time}"


@dataclass(frozen=True)
class ProcessInfo:
    identity: ProcessIdentity
    ppid: int
    comm: str
    executable: str


def _read_process(pid: int) -> ProcessInfo:
    proc_dir = f"/proc/{pid}"
    stat_text = open(f"{proc_dir}/stat", encoding="utf-8").read()
    close_paren = stat_text.rfind(")")
    if close_paren < 0:
        raise ValueError("malformed proc stat")
    comm = stat_text[stat_text.find("(") + 1 : close_paren]
    fields = stat_text[close_paren + 2 :].split()
    ppid = int(fields[1])
    start_time = int(fields[19])
    status = open(f"{proc_dir}/status", encoding="utf-8").read().splitlines()
    uid_line = next(line for line in status if line.startswith("Uid:"))
    uid = int(uid_line.split()[1])
    try:
        executable = os.path.realpath(f"{proc_dir}/exe")
    except OSError:
        executable = ""
    return ProcessInfo(ProcessIdentity(pid, uid, start_time), ppid, comm, executable)


def find_agent_process(peer_pid: int, peer_uid: int, executables: tuple[str, ...]) -> ProcessIdentity | None:
    wanted = {os.path.realpath(path) for path in executables}
    pid = peer_pid
    visited: set[int] = set()
    for _ in range(128):
        if pid <= 1 or pid in visited:
            break
        visited.add(pid)
        try:
            info = _read_process(pid)
        except (OSError, ValueError, StopIteration):
            return None
        if info.identity.uid == peer_uid and info.executable in wanted:
            try:
                executable_stat = os.stat(info.executable)
            except OSError:
                return None
            if executable_stat.st_uid == 0 and not executable_stat.st_mode & 0o022:
                return info.identity
        pid = info.ppid
    return None


def process_is_alive(identity: ProcessIdentity) -> bool:
    try:
        return _read_process(identity.pid).identity == identity
    except (OSError, ValueError, StopIteration):
        return False
