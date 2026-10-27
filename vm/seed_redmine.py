#!/usr/bin/env python3
"""Populate the lab Redmine with the project described by lab/project.json."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
STATE = ROOT / ".vm"
PROJECT_JSON = ROOT / "lab" / "project.json"
SETUP_SCRIPT = ROOT / "lab" / "setup_redmine.rb"
HOST_CREDENTIALS = STATE / "team-credentials.json"

REMOTE_PROJECT = "/root/opi-project.json"
REMOTE_SETUP = "/root/opi-setup.rb"
REMOTE_CREDENTIALS = "/root/opi-team-credentials.json"


def load_manage():
    spec = importlib.util.spec_from_file_location("opi_vm_manage", ROOT / "vm" / "manage.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def ssh(manage, command, *, input_data=None, capture=False, check=True):
    return subprocess.run(
        manage.ssh_args() + [command],
        input=input_data,
        capture_output=capture,
        check=check,
    )


def upload(manage, remote_path, content):
    # Keep the uploaded file private even before the setup script touches it.
    ssh(manage, f"sudo sh -c 'umask 077; cat > {remote_path}'", input_data=content)


def main():
    if not PROJECT_JSON.is_file() or not SETUP_SCRIPT.is_file():
        sys.exit("Не найдены lab/project.json или lab/setup_redmine.rb.")
    if not (STATE / "id_ed25519").is_file():
        sys.exit("Нет SSH-ключа .vm/id_ed25519. Сначала выполните make vm-up.")

    manage = load_manage()
    if not manage.running():
        sys.exit("ВМ остановлена. Сначала выполните make vm-up и дождитесь READY.")

    probe = ssh(
        manage,
        "test -f /var/lib/opi-redmine-ready",
        capture=True,
        check=False,
    )
    if probe.returncode != 0:
        sys.exit("Redmine ещё не готов. Дождитесь READY в make vm-logs.")

    upload(manage, REMOTE_PROJECT, PROJECT_JSON.read_bytes())
    upload(manage, REMOTE_SETUP, SETUP_SCRIPT.read_bytes())

    # When migrating to another PC, a copied credentials file restores the
    # existing team passwords instead of generating new ones.
    if HOST_CREDENTIALS.is_file():
        upload(manage, REMOTE_CREDENTIALS, HOST_CREDENTIALS.read_bytes())
        print("Использую существующие пароли из .vm/team-credentials.json.")

    ssh(
        manage,
        "sudo bash -c 'cd /opt/redmine; set -a; source .env.production; set +a; "
        "bundle _2.5.23_ exec rails runner -e production /root/opi-setup.rb'",
    )

    result = ssh(manage, f"sudo cat {REMOTE_CREDENTIALS}", capture=True)
    try:
        remote_credentials = json.loads(result.stdout.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        sys.exit(f"Не удалось прочитать {REMOTE_CREDENTIALS}: {error}")

    # Keep credentials for accounts that are not part of this run, but let the
    # values from the target Redmine win for accounts it just created.
    credentials = {}
    if HOST_CREDENTIALS.is_file():
        try:
            credentials = json.loads(HOST_CREDENTIALS.read_text())
        except json.JSONDecodeError:
            credentials = {}
    credentials.update(remote_credentials)

    STATE.chmod(0o700)
    HOST_CREDENTIALS.write_text(
        json.dumps(credentials, ensure_ascii=False, indent=2) + "\n"
    )
    HOST_CREDENTIALS.chmod(0o600)

    print("Redmine заполнен проектом pc-assembly-34.")
    print("Пароли участников: .vm/team-credentials.json (0600, не коммитить).")
    print("Проверка: make vm-check; вход: http://localhost:8082")


if __name__ == "__main__":
    os.umask(0o077)
    try:
        main()
    except subprocess.CalledProcessError as error:
        sys.exit(f"Команда завершилась с ошибкой (код {error.returncode}). Смотрите вывод выше.")
