#!/usr/bin/env python3
"""HTTP wrapper around davidteather/TikTok-Api (MIT, 6.5k+ stars, last push 2026-07).

Public keyword → user search only. Does not log in or send DMs.
Upstream: https://github.com/davidteather/TikTok-Api
"""

from __future__ import annotations

import asyncio
import json
import os
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
    return {
        "platform": "tiktok",
        "username": unique,
        "uniqueId": unique,
        "nickname": info.get("nickname") or unique,
        "signature": info.get("signature") or "",
        "secUid": info.get("secUid") or info.get("sec_uid") or "",
        "id": str(info.get("id") or ""),
        "verified": bool(info.get("verified")),
        "followerCount": int(stats.get("followerCount") or info.get("followerCount") or 0),
        "avatar": info.get("avatarLarger") or info.get("avatarThumb") or "",
        "homepageUrl": f"https://www.tiktok.com/@{unique}" if unique else "",
        "source": "tiktok-api",
    }


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
}


def _headed() -> bool:
    return os.environ.get("TIKTOK_HEADED", "").strip() in {"1", "true", "yes"} or bool(
        os.environ.get("DISPLAY")
    )


async def _users_from_search_page(page, keyword: str, count: int) -> list[dict]:
    url = "https://www.tiktok.com/search/user?q=" + quote(keyword)
    await page.goto(url, wait_until="domcontentloaded", timeout=90000)
    await asyncio.sleep(5)
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
    users: list[dict] = []
    seen: set[str] = set()
    for row in raw or []:
        handle = str(row.get("uniqueId") or "").strip()
        key = handle.lower()
        if key in seen or key in _SKIP_HANDLES:
            continue
        seen.add(key)
        users.append(
            {
                "platform": "tiktok",
                "username": handle,
                "uniqueId": handle,
                "nickname": row.get("nickname") or handle,
                "signature": row.get("signature") or "",
                "secUid": "",
                "id": "",
                "verified": False,
                "followerCount": 0,
                "avatar": "",
                "homepageUrl": f"https://www.tiktok.com/@{handle}",
                "source": "tiktok-api-page",
            }
        )
        if len(users) >= count:
            break
    return users


async def search_users(keyword: str, count: int) -> list[dict]:
    if TikTokApi is None:
        raise RuntimeError("TikTokApi is not installed; pip install TikTokApi")

    users: list[dict] = []
    seen: set[str] = set()
    tokens = [MS_TOKEN] if MS_TOKEN else None
    chrome = os.environ.get("TIKTOK_CHROME", "").strip() or None
    async with TikTokApi() as api:
        await api.create_sessions(
            ms_tokens=tokens,
            num_sessions=1,
            sleep_after=3,
            browser=BROWSER,
            headless=not _headed(),
            timeout=90000,
            executable_path=chrome,
        )
        try:
            async for user in api.search.users(keyword, count=count):
                payload = _user_payload(user)
                handle = (payload.get("uniqueId") or "").lower()
                if not handle or handle in seen:
                    continue
                seen.add(handle)
                users.append(payload)
                if len(users) >= count:
                    break
        except Exception as exc:  # noqa: BLE001
            print("tiktok api search fallback:", exc)
        if len(users) < count and api.sessions:
            page_users = await _users_from_search_page(api.sessions[0].page, keyword, count)
            for payload in page_users:
                handle = (payload.get("uniqueId") or "").lower()
                if not handle or handle in seen:
                    continue
                seen.add(handle)
                users.append(payload)
                if len(users) >= count:
                    break
    return users


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
                },
            )
            return

        if parsed.path != "/search/users":
            self._json(404, {"error": "not found", "users": []})
            return

        qs = parse_qs(parsed.query)
        keyword = (qs.get("q") or [""])[0].strip()
        count = max(1, min(int((qs.get("count") or ["10"])[0] or 10), 30))
        if not keyword:
            self._json(400, {"error": "q is required", "users": []})
            return

        try:
            users = asyncio.run(search_users(keyword, count))
        except Exception as exc:  # noqa: BLE001
            self._json(
                502,
                {
                    "error": str(exc),
                    "users": [],
                    "source": "tiktok-api",
                    "warnings": [
                        "TikTok-Api 需要可用的 Chromium + 可选 TIKTOK_MS_TOKEN（该 token 需在网页上先搜过一次）"
                    ],
                },
            )
            return

        self._json(200, {"users": users, "source": "tiktok-api"})


def main() -> None:
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"TikTok-Api sidecar listening on {HOST}:{PORT}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()
