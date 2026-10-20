#!/usr/bin/env python3
"""Local QEMU/KVM lab VM. State and credentials stay in ignored .vm/."""

import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
STATE = ROOT / ".vm"
IMAGE = "ubuntu-24.04-minimal-cloudimg-amd64.img"
IMAGE_URL = "https://cloud-images.ubuntu.com/minimal/releases/noble/release-20261017/"
IMAGE_SHA256 = "b3296484e362ba2e713489a5de6225c719d9500962bde25d15f2f21e512c7658"
WEB_PORT = 8082
SSH_PORT = 2222


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def ssh_args():
    return ["ssh", "-i", str(STATE / "id_ed25519"), "-p", str(SSH_PORT),
            "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=accept-new",
            "-o", f"UserKnownHostsFile={STATE / 'known_hosts'}",
            "-o", "ConnectTimeout=5", "-o", "BatchMode=yes", "student@127.0.0.1"]


def running():
    try:
        pid = int((STATE / "qemu.pid").read_text().strip())
        command = Path(f"/proc/{pid}/cmdline").read_bytes().split(b"\0")
        return b"opi-redmine-vm" in command and str(STATE / "disk.qcow2").encode() in b" ".join(command)
    except (OSError, ValueError):
        return False


def private_write(path, content):
    path.write_text(content)
    path.chmod(0o600)


def prepare():
    for executable in ("qemu-system-x86_64", "qemu-img", "cloud-localds", "ssh-keygen", "curl"):
        if not shutil.which(executable):
            sys.exit(f"Не найден {executable}. См. vm/README.md: зависимости хоста.")
    STATE.mkdir(mode=0o700, exist_ok=True)
    STATE.chmod(0o700)
    if (STATE / "disk.qcow2").exists():
        if not (STATE / "seed.img").exists():
            sys.exit("У существующего диска нет seed.img. Восстановите .vm из резервной копии.")
        return
    if shutil.disk_usage(STATE).free < 5 * 1024**3:
        sys.exit("Для первоначальной установки нужно хотя бы 5 ГБ свободного места.")
    image = STATE / IMAGE
    if not image.exists():
        print("Скачивание официального образа Ubuntu 24.04…", flush=True)
        run("curl", "-fL", "--retry", "3", "--max-time", "900", IMAGE_URL + IMAGE,
            "-o", str(image.with_suffix(".partial")))
        image.with_suffix(".partial").rename(image)
    with image.open("rb") as source:
        digest = hashlib.file_digest(source, "sha256").hexdigest()
    if digest != IMAGE_SHA256:
        sys.exit("SHA-256 образа не совпадает. Установка остановлена.")
    key = STATE / "id_ed25519"
    if not key.exists():
        run("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "opi-redmine-vm", "-f", str(key))
    credentials_file = STATE / "credentials.json"
    if credentials_file.exists():
        credentials = json.loads(credentials_file.read_text())
    else:
        credentials = {"admin_password": secrets.token_urlsafe(18),
                       "db_password": secrets.token_hex(24),
                       "secret_key_base": secrets.token_hex(64)}
        private_write(credentials_file, json.dumps(credentials, indent=2) + "\n")
        private_write(STATE / "credentials.txt", "URL: http://localhost:8082\nLogin: admin\nPassword: "
                      + credentials["admin_password"] + "\nSSH: make vm-ssh (user student)\n")
    archive = io.BytesIO()
    with tarfile.open(fileobj=archive, mode="w:gz") as theme:
        theme.add(ROOT / "themes/bleuclair", arcname="bleuclair")
    config = {
        "hostname": "opi-redmine-vm", "manage_etc_hosts": True,
        "timezone": "Europe/Moscow", "ssh_pwauth": False,
        "users": [{"name": "student", "gecos": "OPI lab student", "shell": "/bin/bash",
                   "groups": ["adm", "sudo"], "sudo": "ALL=(ALL) NOPASSWD:ALL",
                   "lock_passwd": True, "ssh_authorized_keys": [key.with_suffix(".pub").read_text().strip()]}],
        "write_files": [
            {"path": "/root/provision-redmine.sh", "permissions": "0700",
             "content": (ROOT / "vm/provision.sh").read_text()},
            {"path": "/root/redmine-credentials.json", "permissions": "0600", "content": json.dumps(credentials)},
            {"path": "/root/bleuclair.tar.gz", "permissions": "0600", "encoding": "b64",
             "content": base64.b64encode(archive.getvalue()).decode()},
        ],
        "runcmd": [["bash", "/root/provision-redmine.sh"]],
        "final_message": "OPI VM cloud-init finished after $UPTIME seconds",
    }
    private_write(STATE / "user-data", "#cloud-config\n" + json.dumps(config, indent=2) + "\n")
    private_write(STATE / "meta-data", "instance-id: opi-redmine-v1\nlocal-hostname: opi-redmine-vm\n")
    run("cloud-localds", str(STATE / "seed.img"), str(STATE / "user-data"), str(STATE / "meta-data"))
    run("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", str(image), str(STATE / "disk.qcow2"), "16G")


def up():
    if running():
        print("ВМ уже запущена: http://localhost:8082")
        return
    if not os.access("/dev/kvm", os.R_OK | os.W_OK):
        sys.exit("Нет доступа к /dev/kvm. См. vm/README.md.")
    first_boot = not (STATE / "disk.qcow2").exists()
    prepare()
    for port in (WEB_PORT, SSH_PORT):
        with socket.socket() as check:
            # Finished HTTP connections can remain in TIME_WAIT after shutdown.
            # Match QEMU's address reuse while still rejecting active listeners.
            check.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                check.bind(("127.0.0.1", port))
            except OSError:
                sys.exit(f"Порт {port} занят. Остановите использующий его процесс.")
    # A TCP QMP socket is unnecessary; the local Unix socket is inside private .vm/.
    monitor = STATE / "monitor.sock"
    monitor.unlink(missing_ok=True)
    memory = os.environ.get("VM_MEMORY_MB", "1536")
    run("qemu-system-x86_64", "-name", "opi-redmine-vm", "-machine", "q35,accel=kvm", "-cpu", "host",
        "-smp", "2", "-m", memory, "-drive", f"file={STATE / 'disk.qcow2'},if=virtio,format=qcow2",
        "-drive", f"file={STATE / 'seed.img'},if=virtio,format=raw,readonly=on",
        "-nic", f"user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:{WEB_PORT}-:80,hostfwd=tcp:127.0.0.1:{SSH_PORT}-:22",
        "-display", "none", "-serial", f"file:{STATE / 'serial.log'}",
        "-qmp", f"unix:{monitor},server=on,wait=off", "-pidfile", str(STATE / "qemu.pid"), "-daemonize")
    print("ВМ запущена. Redmine: http://localhost:8082; SSH: make vm-ssh", flush=True)
    if first_boot:
        print("Первая установка выполняется в фоне. Ход установки: make vm-logs")
    else:
        print("Загружается существующая ВМ. Состояние: make vm-status")


def qmp_powerdown():
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(str(STATE / "monitor.sock"))
        stream = connection.makefile("rwb")
        stream.readline()
        for command in ("qmp_capabilities", "system_powerdown"):
            stream.write((json.dumps({"execute": command}) + "\n").encode())
            stream.flush()
            while True:
                response = json.loads(stream.readline())
                if "return" in response:
                    break
                if "error" in response:
                    raise RuntimeError(response["error"])


def down():
    if not running():
        print("ВМ уже остановлена.")
        return
    qmp_powerdown()
    for _ in range(45):
        if not running():
            print("ВМ штатно остановлена. Данные сохранены.")
            return
        time.sleep(1)
    sys.exit("ВМ ещё завершает работу. Проверьте make vm-status; принудительная остановка не выполнялась.")


def status():
    if not running():
        print("ВМ остановлена. Запуск: make vm-up")
        return
    print("ВМ работает (QEMU/KVM): 2 CPU; HTTP 8082; SSH 2222.")
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{WEB_PORT}/", timeout=5) as response:
            page = response.read().decode()
        print("Redmine доступен: http://localhost:8082; Bleuclair: "
              + ("подключена" if "theme-Bleuclair" in page else "не обнаружена"))
    except (OSError, urllib.error.URLError):
        print("Redmine пока недоступен. Установка/загрузка: make vm-logs")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["up", "down", "status", "ssh", "logs"])
    parser.add_argument("command", nargs=argparse.REMAINDER)
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.action in ("up", "down", "status"):
        globals()[arguments.action]()
    elif arguments.action == "ssh":
        if not running():
            sys.exit("ВМ остановлена. Выполните make vm-up.")
        os.execvp("ssh", ssh_args() + arguments.command)
    elif arguments.action == "logs":
        if not running():
            sys.exit("ВМ остановлена. Журнал загрузки: .vm/serial.log")
        os.execvp("ssh", ssh_args() + ["sudo tail -n 50 -f /var/log/opi-provision.log"])


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, OSError, RuntimeError) as error:
        sys.exit(str(error))
