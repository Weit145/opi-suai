#!/usr/bin/env python3
"""Capture Redmine administration pages for the LR2 report.

The script logs in as the local administrator, fetches the rendered HTML of
selected pages and lets headless Google Chrome take a screenshot.  Credentials
are read from the ignored .vm directory and are never printed.
"""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

import requests
from bs4 import BeautifulSoup

ROOT = Path(__file__).resolve().parent.parent
BASE = "http://localhost:8082"
OUT = ROOT / "docs" / "images"
TMP = Path("/tmp/opi-redmine-capture")
CREDENTIALS = ROOT / ".vm" / "credentials.json"
TEAM_CREDENTIALS = ROOT / ".vm" / "team-credentials.json"

PAGES = {
    "redmine-roles": ("/roles", 1440, 650),
    "redmine-trackers": ("/trackers", 1440, 600),
    "redmine-statuses": ("/issue_statuses", 1440, 600),
}


def login(username: str, password: str) -> requests.Session:
    session = requests.Session()
    response = session.get(f"{BASE}/login", timeout=20)
    response.raise_for_status()
    page = BeautifulSoup(response.text, "html.parser")
    token = page.find("input", {"name": "authenticity_token"})
    if token is None:
        raise RuntimeError("login form has no authenticity_token")
    response = session.post(
        f"{BASE}/login",
        data={
            "username": username,
            "password": password,
            "authenticity_token": token.get("value", ""),
            "back_url": f"{BASE}/",
        },
        timeout=20,
    )
    response.raise_for_status()
    if "Вошли как" not in response.text and "logout" not in response.text.lower():
        raise RuntimeError("Redmine login failed")
    return session


def find_id(session: requests.Session, path: str, label: str, pattern: str) -> int:
    response = session.get(f"{BASE}{path}", timeout=30)
    response.raise_for_status()
    page = BeautifulSoup(response.text, "html.parser")
    for link in page.find_all("a", href=True):
        if label in link.get_text(" ", strip=True):
            match = re.search(pattern, link["href"])
            if match:
                return int(match.group(1))
    raise RuntimeError(f"cannot find {label!r} on {path}")


def capture(session: requests.Session, name: str, path: str, width: int, height: int,
            allow_forbidden: bool = False) -> None:
    response = session.get(f"{BASE}{path}", timeout=30)
    if allow_forbidden and response.status_code == 403:
        pass
    else:
        response.raise_for_status()
    html = response.text
    if "<head>" in html:
        html = html.replace("<head>", f'<head><base href="{BASE}/">', 1)
    else:
        raise RuntimeError(f"no <head> in {path}")
    html_path = TMP / f"{name}.html"
    html_path.write_text(html, encoding="utf-8")
    png_path = OUT / f"{name}.png"
    command = [
        "google-chrome",
        "--headless=new",
        "--no-sandbox",
        "--disable-gpu",
        "--disable-dev-shm-usage",
        "--hide-scrollbars",
        "--no-first-run",
        "--no-default-browser-check",
        "--force-device-scale-factor=1",
        f"--window-size={width},{height}",
        "--virtual-time-budget=5000",
        f"--screenshot={png_path}",
        html_path.as_uri(),
    ]
    subprocess.run(command, check=True, stdout=subprocess.PIPE,
                   stderr=subprocess.PIPE, timeout=120)
    print(f"{png_path.relative_to(ROOT)}  {width}x{height}")


def main() -> None:
    TMP.mkdir(parents=True, exist_ok=True)
    OUT.mkdir(parents=True, exist_ok=True)
    admin = json.loads(CREDENTIALS.read_text(encoding="utf-8"))["admin_password"]
    team = json.loads(TEAM_CREDENTIALS.read_text(encoding="utf-8"))

    session = login("admin", admin)
    role_id = find_id(session, "/roles", "Руководитель проекта", r"/roles/(\d+)/edit")
    tracker_id = find_id(session, "/trackers", "Конфигурирование", r"/trackers/(\d+)/edit")
    pages = dict(PAGES)
    pages["redmine-workflow"] = (
        f"/workflows/edit?role_id={role_id}&tracker_id={tracker_id}",
        1440,
        1000,
    )
    for name, (path, width, height) in pages.items():
        capture(session, name, path, width, height)

    specialist = login("kochnev-do", team["kochnev-do"]["password"])
    capture(specialist, "redmine-forbidden", "/admin", 1440, 650,
            allow_forbidden=True)

    session = login("admin", admin)
    capture(session, "redmine-users", "/users", 1440, 650)


if __name__ == "__main__":
    main()
