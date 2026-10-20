#!/usr/bin/env python3
"""Check the running lab VM through HTTP, including administrator login."""

from html.parser import HTMLParser
import http.cookiejar
import json
from pathlib import Path
import re
import sys
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:8082"


class Page(HTMLParser):
    def __init__(self, html):
        super().__init__()
        self.token = None
        self.assets = []
        self.feed(html)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "input" and attrs.get("name") == "authenticity_token":
            self.token = attrs.get("value")
        if tag == "link" and attrs.get("rel") == "stylesheet":
            self.assets.append(attrs["href"])
        if tag == "script" and attrs.get("src", "").startswith("/assets/"):
            self.assets.append(attrs["src"])


def main():
    credentials = json.loads((Path(__file__).resolve().parent.parent / ".vm/credentials.json").read_text())
    browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def get(path):
        with browser.open(urllib.parse.urljoin(BASE, path), timeout=30) as response:
            return response.read().decode("utf-8")

    home = get("/")
    assert "theme-Bleuclair" in home, "Bleuclair не подключена"
    assets = set(Page(home).assets)
    assert any("themes/bleuclair/" in url for url in assets), "Нет ресурсов темы"
    for asset in list(assets):
        content = get(asset)
        if asset.endswith(".css"):
            for imported in re.findall(r'@import\s+url\([\'"]?([^\'"\s)]+)', content):
                get(urllib.parse.urljoin(asset, imported))
    print(f"OK: HTTP, Bleuclair, CSS/JS и CSS-импорты ({len(assets)} ресурсов страницы)")
    token = Page(get("/login")).token
    assert token, "Не найден CSRF-токен формы входа"
    data = urllib.parse.urlencode({"authenticity_token": token, "username": "admin",
                                   "password": credentials["admin_password"]}).encode()
    with browser.open(BASE + "/login", data=data, timeout=30) as response:
        response.read()
    account = get("/my/account")
    assert 'id="loggedas"' in account, "Вход администратора не выполнен"
    settings = get("/settings?tab=display")
    assert 'name="settings[ui_theme]"' in settings, "Нет доступа к настройкам администратора"
    assert re.search(r'<option(?=[^>]*value="bleuclair")(?=[^>]*selected="selected")[^>]*>', settings), "Тема не сохранена"
    print("OK: вход admin, доступ к настройкам, сохранённая тема Bleuclair")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, AssertionError) as error:
        sys.exit(f"Проверка не пройдена: {error}")
