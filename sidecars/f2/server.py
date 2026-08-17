#!/usr/bin/env python3
"""HTTP wrapper around Johnserf-Seed/f2 (Apache-2.0, 2.6k stars, last push 2026-04).

TikTok: fetch_search_videos → unique authors (keyword → people/homepages).
Douyin: fetch_user_profile when q is a homepage URL / sec_uid.
  Keyword user search is still marked 🔵 upstream; do not reimplement it here.

Upstream: https://github.com/Johnserf-Seed/f2
"""

from __future__ import annotations

import asyncio
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

try:
    from f2.apps.tiktok.crawler import TiktokCrawler
    from f2.apps.tiktok.model import PostSearch
except Exception:  # pragma: no cover
    # f2 generates msToken at import time; missing/invalid cookie must not kill the process.
    TiktokCrawler = None
    PostSearch = None

try:
    from f2.apps.douyin.handler import DouyinHandler
    from f2.apps.douyin.utils import SecUserIdFetcher
except Exception:  # pragma: no cover
    DouyinHandler = None
    SecUserIdFetcher = None


HOST = os.environ.get("SIDECAR_HOST", "0.0.0.0")
PORT = int(os.environ.get("SIDECAR_PORT", "8092"))
TIKTOK_COOKIE = os.environ.get("F2_TIKTOK_COOKIE", "").strip()
DOUYIN_COOKIE = os.environ.get("F2_DOUYIN_COOKIE", "").strip()


def _tiktok_kwargs() -> dict:
    return {
        "headers": {
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
            "Referer": "https://www.tiktok.com/",
        },
        "cookie": TIKTOK_COOKIE,
        "proxies": {"http://": None, "https://": None},
        "timeout": 3,
    }


def _douyin_kwargs() -> dict:
    return {
        "headers": {
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
            "Referer": "https://www.douyin.com/",
        },
        "cookie": DOUYIN_COOKIE,
        "proxies": {"http://": None, "https://": None},
        "timeout": 3,
    }


def _author_from_item(item: dict) -> dict | None:
    author = item.get("author") or {}
    unique = author.get("uniqueId") or author.get("unique_id") or ""
    if not unique:
        return None
    return {
        "platform": "tiktok",
        "username": unique,
        "uniqueId": unique,
        "nickname": author.get("nickname") or unique,
        "signature": author.get("signature") or (item.get("desc") or "")[:160],
        "secUid": author.get("secUid") or "",
        "id": str(author.get("id") or ""),
        "verified": bool(author.get("verified")),
        "followerCount": int((author.get("stats") or {}).get("followerCount") or 0),
        "avatar": author.get("avatarLarger") or author.get("avatarThumb") or "",
        "homepageUrl": f"https://www.tiktok.com/@{unique}",
        "source": "f2-tiktok",
    }


async def search_tiktok(keyword: str, count: int) -> list[dict]:
    if TiktokCrawler is None or PostSearch is None:
        raise RuntimeError("f2 is not installed; pip install f2")

    users: list[dict] = []
    seen: set[str] = set()
    kwargs = _tiktok_kwargs()
    offset = 0
    search_id = ""
    from urllib.parse import quote

    async with TiktokCrawler(kwargs) as crawler:
        while len(users) < count:
            params = PostSearch(
                keyword=quote(keyword, safe=""),
                offset=offset,
                count=min(12, count),
                search_id=search_id,
            )
            response = await crawler.fetch_post_search(params)
            items = (response or {}).get("item_list") or (response or {}).get("itemList") or []
            if not items:
                break
            for item in items:
                payload = _author_from_item(item)
                if not payload:
                    continue
                handle = payload["uniqueId"].lower()
                if handle in seen:
                    continue
                seen.add(handle)
                users.append(payload)
                if len(users) >= count:
                    break
            offset = int((response or {}).get("cursor") or offset + len(items))
            search_id = str(((response or {}).get("extra") or {}).get("logid") or search_id)
            if not (response or {}).get("has_more"):
                break
    return users


def _looks_like_douyin_target(q: str) -> bool:
    ql = q.lower()
    return "douyin.com" in ql or q.startswith("MS4wLjAB") or "/user/" in ql


async def search_douyin(keyword: str, count: int) -> tuple[list[dict], list[str]]:
    warnings: list[str] = []
    if DouyinHandler is None:
        raise RuntimeError("f2 is not installed; pip install f2")

    if not _looks_like_douyin_target(keyword):
        warnings.append(
            "f2 的抖音「搜索用户」fetch_search_users 上游仍为 🔵 未完成，本 sidecar 不自研补齐。"
            "请改搜 TikTok，或传入抖音主页 URL / sec_uid，或配置 TIKHUB_API_TOKEN。"
        )
        return [], warnings

    sec_uid = keyword
    if "http" in keyword.lower() and SecUserIdFetcher is not None:
        sec_uid = await SecUserIdFetcher.get_secuid(keyword)

    handler = DouyinHandler(_douyin_kwargs())
    user = await handler.fetch_user_profile(sec_uid)
    raw = {}
    if hasattr(user, "_to_dict"):
        raw = user._to_dict() or {}
    nickname = getattr(user, "nickname", None) or raw.get("nickname") or sec_uid
    signature = getattr(user, "signature", None) or raw.get("signature") or ""
    unique = getattr(user, "unique_id", None) or raw.get("unique_id") or sec_uid
    payload = {
        "platform": "douyin",
        "username": unique,
        "uniqueId": unique,
        "nickname": nickname,
        "signature": signature,
        "secUid": sec_uid,
        "id": str(getattr(user, "uid", "") or raw.get("uid") or ""),
        "verified": bool(getattr(user, "verification_type", 0)),
        "followerCount": int(getattr(user, "follower_count", 0) or 0),
        "avatar": "",
        "homepageUrl": f"https://www.douyin.com/user/{sec_uid}",
        "source": "f2-douyin",
    }
    return [payload], warnings


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
                    "project": "Johnserf-Seed/f2",
                    "f2_tiktok": TiktokCrawler is not None,
                    "f2_douyin": DouyinHandler is not None,
                },
            )
            return

        if parsed.path != "/search/users":
            self._json(404, {"error": "not found", "users": []})
            return

        qs = parse_qs(parsed.query)
        keyword = (qs.get("q") or [""])[0].strip()
        platform = ((qs.get("platform") or ["tiktok"])[0] or "tiktok").lower()
        count = max(1, min(int((qs.get("count") or ["10"])[0] or 10), 30))
        if not keyword:
            self._json(400, {"error": "q is required", "users": []})
            return

        try:
            if platform == "douyin":
                users, warnings = asyncio.run(search_douyin(keyword, count))
                self._json(
                    200,
                    {"users": users, "warnings": warnings, "source": "f2-douyin"},
                )
                return
            users = asyncio.run(search_tiktok(keyword, count))
            self._json(200, {"users": users, "source": "f2-tiktok"})
        except Exception as exc:  # noqa: BLE001
            self._json(
                502,
                {
                    "error": str(exc),
                    "users": [],
                    "source": "f2",
                    "warnings": [
                        "f2 对 TikTok 搜视频抽作者通常需要 F2_TIKTOK_COOKIE；抖音主页需要 F2_DOUYIN_COOKIE"
                    ],
                },
            )


def main() -> None:
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"f2 sidecar listening on {HOST}:{PORT}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()
