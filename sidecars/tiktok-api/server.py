#!/usr/bin/env python3
"""HTTP wrapper around davidteather/TikTok-Api (MIT, 6.5k+ stars, last push 2026-07).

Public keyword → user / tag / related-profile search only. Does not log in or send DMs.
A persistent Chromium session is reused so harvest is not one-browser-per-query.
Upstream: https://github.com/davidteather/TikTok-Api
"""

from __future__ import annotations

import asyncio
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, quote, urlparse

try:
    from TikTokApi import TikTokApi
except ImportError:  # pragma: no cover
    TikTokApi = None


HOST = os.environ.get("SIDECAR_HOST", "0.0.0.0")
PORT = int(os.environ.get("SIDECAR_PORT", "8091"))
MS_TOKEN = os.environ.get("TIKTOK_MS_TOKEN", "").strip() or None
BROWSER = os.environ.get("TIKTOK_BROWSER", "chromium")
# Signed search.users is empty on datacenter IPs; skip it unless a token is set.
SKIP_SIGNED = os.environ.get("TIKTOK_SKIP_SIGNED", "1").strip() not in {"0", "false", "no"}
SESSIONS = max(1, min(int(os.environ.get("TIKTOK_SESSIONS", "2") or 2), 4))

_SKIP_HANDLES = {
    "",
    "foryou",
    "following",
    "live",
    "search",
    "explore",
    "login",
    "signup",
    "about",
    "privacy",
    "tiktok",
    "discover",
    "music",
    "tag",
    "video",
    "photo",
    "effect",
    "place",
}

_loop: asyncio.AbstractEventLoop | None = None
_api = None
_rr = 0
_rr_lock: asyncio.Lock | None = None
_api_lock: asyncio.Lock | None = None


def _headed() -> bool:
    return os.environ.get("TIKTOK_HEADED", "").strip() in {"1", "true", "yes"} or bool(
        os.environ.get("DISPLAY")
    )


def _user_row(handle: str, nickname: str, signature: str, source: str) -> dict:
    return {
        "platform": "tiktok",
        "username": handle,
        "uniqueId": handle,
        "nickname": nickname or handle,
        "signature": signature or "",
        "secUid": "",
        "id": "",
        "verified": False,
        "followerCount": 0,
        "avatar": "",
        "homepageUrl": f"https://www.tiktok.com/@{handle}",
        "source": source,
    }


def _user_payload(user) -> dict:
    data = {}
    if hasattr(user, "as_dict"):
        data = user.as_dict or {}
    info = data.get("userInfo", {}).get("user") or data.get("user") or data
    stats = data.get("userInfo", {}).get("stats") or data.get("stats") or {}
    unique = (
        info.get("uniqueId")
        or info.get("unique_id")
        or getattr(user, "username", "")
        or ""
    )
    row = _user_row(
        unique,
        info.get("nickname") or unique,
        info.get("signature") or "",
        "tiktok-api",
    )
    row["secUid"] = info.get("secUid") or info.get("sec_uid") or ""
    row["id"] = str(info.get("id") or "")
    row["verified"] = bool(info.get("verified"))
    row["followerCount"] = int(stats.get("followerCount") or info.get("followerCount") or 0)
    row["avatar"] = info.get("avatarLarger") or info.get("avatarThumb") or ""
    return row


async def _extract_handles(page, count: int, source: str, skip: set[str] | None = None) -> list[dict]:
    raw = await page.evaluate(
        """() => {
          const out = [];
          const seen = new Set();
          for (const a of document.querySelectorAll("a[href*='/@']")) {
            const href = a.getAttribute("href") || "";
            const m = href.match(/\\/@([A-Za-z0-9._]+)/);
            if (!m) continue;
            const handle = m[1];
            const key = handle.toLowerCase();
            if (seen.has(key)) continue;
            seen.add(key);
            const text = (a.innerText || a.textContent || "").trim();
            const lines = text.split(/\\n+/).map(s => s.trim()).filter(Boolean);
            out.push({
              uniqueId: handle,
              nickname: lines[0] || handle,
              signature: lines.slice(1).join(" ").slice(0, 160)
            });
          }
          return out;
        }"""
    )
    skip = {s.lower() for s in (skip or set())}
    users: list[dict] = []
    seen: set[str] = set()
    for row in raw or []:
        handle = str(row.get("uniqueId") or "").strip()
        key = handle.lower()
        if key in seen or key in _SKIP_HANDLES or key in skip:
            continue
        seen.add(key)
        users.append(_user_row(handle, row.get("nickname") or handle, row.get("signature") or "", source))
        if len(users) >= count:
            break
    return users


async def _open_and_scroll(page, url: str, scrolls: int) -> None:
    await page.goto(url, wait_until="domcontentloaded", timeout=90000)
    await asyncio.sleep(2.4)
    for _ in range(scrolls):
        await page.mouse.wheel(0, 3600)
        await asyncio.sleep(0.85)


async def _ensure_api():
    global _api
    if TikTokApi is None:
        raise RuntimeError("TikTokApi is not installed; pip install TikTokApi")
    async with _api_lock:
        if _api is not None and getattr(_api, "sessions", None):
            return _api
        if _api is not None:
            try:
                await _api.__aexit__(None, None, None)
            except Exception:
                pass
            _api = None
        api = TikTokApi()
        await api.__aenter__()
        tokens = [MS_TOKEN] if MS_TOKEN else None
        chrome = os.environ.get("TIKTOK_CHROME", "").strip() or None
        await api.create_sessions(
            ms_tokens=tokens,
            num_sessions=SESSIONS,
            sleep_after=1,
            browser=BROWSER,
            headless=not _headed(),
            timeout=90000,
            executable_path=chrome,
        )
        _api = api
        print(f"tiktok session ready sessions={len(api.sessions)} headed={_headed()}")
        return api


async def _next_page():
    global _rr
    api = await _ensure_api()
    async with _rr_lock:
        i = _rr % max(1, len(api.sessions))
        _rr += 1
        return api.sessions[i].page


async def search_users(keyword: str, count: int) -> list[dict]:
    users: list[dict] = []
    seen: set[str] = set()
    if not SKIP_SIGNED or MS_TOKEN:
        api = await _ensure_api()
        try:
            async for user in api.search.users(keyword, count=count):
                payload = _user_payload(user)
                handle = (payload.get("uniqueId") or "").lower()
                if not handle or handle in seen:
                    continue
                seen.add(handle)
                users.append(payload)
                if len(users) >= count:
                    return users
        except Exception as exc:  # noqa: BLE001
            print("tiktok api search fallback:", exc)
    page = await _next_page()
    await _open_and_scroll(page, "https://www.tiktok.com/search/user?q=" + quote(keyword), 10)
    for payload in await _extract_handles(page, count, "tiktok-api-page"):
        handle = (payload.get("uniqueId") or "").lower()
        if not handle or handle in seen:
            continue
        seen.add(handle)
        users.append(payload)
        if len(users) >= count:
            break
    return users


async def search_tag(keyword: str, count: int) -> list[dict]:
    slug = "".join(ch for ch in keyword.lower() if ch.isalnum() or ch in "._")
    if not slug:
        return []
    page = await _next_page()
    await _open_and_scroll(page, "https://www.tiktok.com/tag/" + quote(slug), 8)
    return await _extract_handles(page, count, "tiktok-api-tag")


async def search_related(handle: str, count: int) -> list[dict]:
    handle = handle.strip().lstrip("@")
    if not handle:
        return []
    page = await _next_page()
    await _open_and_scroll(page, "https://www.tiktok.com/@" + quote(handle), 6)
    return await _extract_handles(page, count, "tiktok-api-related", skip={handle})


def _run(coro, timeout: float = 120):
    fut = asyncio.run_coroutine_threadsafe(coro, _loop)
    return fut.result(timeout=timeout)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:
        print("[%s] " % self.log_date_time_string() + fmt % args)

    def _json(self, code: int, payload: dict) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        if parsed.path in ("/healthz", "/"):
            self._json(
                200,
                {
                    "ok": True,
                    "project": "davidteather/TikTok-Api",
                    "tiktokapi": TikTokApi is not None,
                    "sessions": SESSIONS,
                    "skipSigned": SKIP_SIGNED,
                },
            )
            return

        qs = parse_qs(parsed.query)
        count = max(1, min(int((qs.get("count") or ["20"])[0] or 20), 80))
        try:
            if parsed.path == "/search/users":
                keyword = (qs.get("q") or [""])[0].strip()
                if not keyword:
                    self._json(400, {"error": "q is required", "users": []})
                    return
                users = _run(search_users(keyword, count))
                self._json(200, {"users": users, "source": "tiktok-api"})
                return
            if parsed.path == "/search/tag":
                keyword = (qs.get("q") or [""])[0].strip()
                if not keyword:
                    self._json(400, {"error": "q is required", "users": []})
                    return
                users = _run(search_tag(keyword, count))
                self._json(200, {"users": users, "source": "tiktok-api-tag"})
                return
            if parsed.path == "/related":
                handle = (qs.get("handle") or qs.get("q") or [""])[0].strip()
                if not handle:
                    self._json(400, {"error": "handle is required", "users": []})
                    return
                users = _run(search_related(handle, count))
                self._json(200, {"users": users, "source": "tiktok-api-related"})
                return
        except Exception as exc:  # noqa: BLE001
            self._json(
                502,
                {
                    "error": str(exc),
                    "users": [],
                    "source": "tiktok-api",
                    "warnings": [
                        "TikTok-Api 需要可用的 Chromium；机房 IP 上签名搜人常空，已改走公开搜索页/话题页"
                    ],
                },
            )
            return

        self._json(404, {"error": "not found", "users": []})


async def _setup() -> None:
    global _rr_lock, _api_lock
    _rr_lock = asyncio.Lock()
    _api_lock = asyncio.Lock()
    await _ensure_api()


def main() -> None:
    global _loop
    _loop = asyncio.new_event_loop()
    threading.Thread(target=_loop.run_forever, daemon=True).start()
    try:
        _run(_setup(), timeout=180)
    except Exception as exc:  # noqa: BLE001
        print("tiktok session warmup failed:", exc)
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"TikTok-Api sidecar listening on {HOST}:{PORT} sessions={SESSIONS}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()
