#!/usr/bin/env python3
"""AHU (ahu.go.id) company directors lookup — optional enrichment.

Requires:
  - AHU_PROXY / --proxy (residential or Cloudflare Worker; datacenter IPs are blocked)
  - playwright (preferred) or the indonesia-civic-stack ahu module

Outputs a single JSON object to stdout:
  {"ok": true, "result": {...}} or {"ok": false, "error": "..."}
"""

from __future__ import annotations

import argparse
import json
import os
import sys


def emit(ok: bool, **kwargs) -> int:
    print(json.dumps({"ok": ok, **kwargs}, ensure_ascii=False))
    return 0 if ok else 2


def try_civic_stack(query: str, proxy: str) -> dict | None:
    try:
        import asyncio

        from civic_stack.ahu.scraper import fetch  # type: ignore
    except Exception:
        return None

    async def _run():
        return await fetch(query, proxy_url=proxy)

    try:
        resp = asyncio.run(_run())
    except Exception as e:
        return {"ok": False, "error": f"civic_stack: {e}"}

    if hasattr(resp, "model_dump"):
        data = resp.model_dump()
    elif isinstance(resp, dict):
        data = resp
    else:
        data = getattr(resp, "__dict__", {})

    status = str(data.get("status") or "").upper()
    result = data.get("result") or data.get("data")
    if result and (data.get("ok") is True or status in ("", "OK", "SUCCESS")):
        return {"ok": True, "result": result}
    err = data.get("detail") or data.get("error") or data.get("message") or "ahu not found"
    return {"ok": False, "error": str(err)}


def try_playwright(query: str, proxy: str) -> dict | None:
    try:
        from playwright.sync_api import sync_playwright
    except Exception:
        return None

    url = "https://ahu.go.id/pencarian/perseroan-terbatas"
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(
                headless=True,
                proxy={"server": proxy} if proxy else None,
            )
            page = browser.new_page()
            page.goto(url, wait_until="domcontentloaded", timeout=45000)
            selectors = [
                "input[type='search']",
                "input[placeholder*='Cari']",
                "input[name='q']",
                "input[type='text']",
            ]
            filled = False
            for sel in selectors:
                loc = page.locator(sel).first
                if loc.count() == 0:
                    continue
                try:
                    loc.fill(query, timeout=5000)
                    loc.press("Enter")
                    filled = True
                    break
                except Exception:
                    continue
            if not filled:
                browser.close()
                return {"ok": False, "error": "could not locate AHU search input"}
            page.wait_for_timeout(5000)
            text = page.inner_text("body")
            browser.close()
            if "cloudflare" in text.lower() and "just a moment" in text.lower():
                return {"ok": False, "error": "cloudflare challenge (need better proxy/camoufox)"}
            return {
                "ok": False,
                "error": "playwright reached AHU but structured directors parse needs civic_stack/camoufox",
                "debug_excerpt": text[:400],
            }
    except Exception as e:
        return {"ok": False, "error": f"playwright: {e}"}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--query", required=True)
    ap.add_argument(
        "--proxy",
        default=os.environ.get("AHU_PROXY")
        or os.environ.get("AHU_PROXY_URL")
        or "",
    )
    args = ap.parse_args()
    if not args.proxy:
        return emit(False, error="AHU_PROXY not set (datacenter IPs blocked by AHU/CF)")

    last_err = "ahu failed"
    for fn in (try_civic_stack, try_playwright):
        out = fn(args.query, args.proxy)
        if out is None:
            continue
        if out.get("ok"):
            return emit(True, result=out.get("result") or {})
        last_err = str(out.get("error") or last_err)

    if last_err == "ahu failed":
        last_err = (
            "no AHU backend: pip install playwright "
            "(and/or indonesia-civic-stack) + set AHU_PROXY"
        )
    return emit(False, error=last_err)


if __name__ == "__main__":
    sys.exit(main())
