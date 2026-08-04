#!/usr/bin/env python3
"""AHU (ahu.go.id) company lookup via Playwright / Crawl4AI / Scrapling.

Uses AHU_PROXY (Clash mixed-port / SOCKS/HTTP). Datacenter IPs often fail.

Current public page: https://ahu.go.id/pencarian/profil-pt
(Old /pencarian/perseroan-terbatas is in maintenance.)

Free layer returns: company name, address, phone, bakum id, transaction history.
Full directors (Profil Lengkap / Profil Terakhir PDF) require AHU paid voucher.

Stdout JSON:
  {"ok": true, "result": {...}, "backend": "playwright"} 
  or {"ok": false, "error": "..."}
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import sys
from typing import Any
from urllib.parse import urlparse

import urllib.request


AHU_PROFIL_PT = "https://ahu.go.id/pencarian/profil-pt"
AHU_HISTORY = "https://ahu.go.id/pencarian/bakum/apiHistoryTransaksi/id/{id}"


def emit_obj(obj: dict[str, Any], code: int = 0) -> int:
    print(json.dumps(obj, ensure_ascii=False))
    return code


def clean_text(s: str) -> str:
    return re.sub(r"\s+", " ", (s or "")).strip()


def parse_search_html(html: str, query: str) -> dict[str, Any] | None:
    """Parse free search result cards from profil-pt HTML."""
    if not html or 'id="hasil_cari"' not in html and "hasil_cari" not in html:
        # still allow match via strong[data-id]
        if 'class="judul' not in html and "beli_profile_lengkap" not in html:
            return None

    companies: list[dict[str, Any]] = []
    # Each card: <strong data-id="..." class="judul ...">NAME</strong> + .alamat/.kabpro/.telp
    for m in re.finditer(
        r'<strong[^>]*data-id=["\'](\d+)["\'][^>]*class=["\'][^"\']*judul[^"\']*["\'][^>]*>([\s\S]*?)</strong>([\s\S]*?)(?=<strong[^>]*data-id=|</section>|$)',
        html,
        flags=re.I,
    ):
        bakum_id = m.group(1)
        name = clean_text(re.sub(r"<[^>]+>", " ", m.group(2)))
        block = m.group(3)
        telp = ""
        alamat = ""
        kabpro = ""
        tm = re.search(r'class=["\']telp["\'][^>]*>([\s\S]*?)</div>', block, re.I)
        if tm:
            telp = clean_text(re.sub(r"<[^>]+>", " ", tm.group(1)))
        am = re.search(r'class=["\']alamat["\'][^>]*>([\s\S]*?)</div>', block, re.I)
        if am:
            alamat = clean_text(re.sub(r"<[^>]+>", " ", am.group(1)))
        km = re.search(r'class=["\']kabpro["\'][^>]*>([\s\S]*?)</div>', block, re.I)
        if km:
            kabpro = clean_text(re.sub(r"<[^>]+>", " ", km.group(1)))
        if not name:
            continue
        companies.append(
            {
                "bakum_id": bakum_id,
                "company_name": name,
                "phone": telp,
                "address": alamat,
                "region": kabpro,
                "domicile": clean_text(f"{alamat}, {kabpro}".strip(", ")),
            }
        )

    if not companies:
        return None

    primary = companies[0]
    return {
        "company_name": primary["company_name"],
        "registration_no": "",
        "legal_form": "Perseroan Terbatas (PT)",
        "legal_status": "FOUND",
        "domicile": primary.get("domicile", ""),
        "phone": primary.get("phone", ""),
        "bakum_id": primary.get("bakum_id", ""),
        "directors": [],
        "commissioners": [],
        "search_hits": companies,
        "source_url": AHU_PROFIL_PT,
        "query": query,
        "note": "免费层：公司名/地址/电话/bakum_id；完整董事需 AHU「Profil Lengkap」付费 voucher",
    }


def fetch_history(bakum_id: str, proxy: str) -> list[dict[str, Any]]:
    if not bakum_id:
        return []
    url = AHU_HISTORY.format(id=bakum_id)
    try:
        opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({"http": proxy, "https": proxy})
        )
        req = urllib.request.Request(
            url,
            headers={
                "User-Agent": "Mozilla/5.0",
                "Referer": AHU_PROFIL_PT,
                "Accept": "application/json,text/plain,*/*",
            },
        )
        with opener.open(req, timeout=20) as resp:
            raw = resp.read().decode("utf-8", "ignore")
        data = json.loads(raw)
        if isinstance(data, list):
            return data[:30]
    except Exception:
        return []
    return []


def playwright_proxy_config(proxy: str) -> dict[str, str]:
    u = urlparse(proxy if "://" in proxy else "http://" + proxy)
    scheme = u.scheme or "http"
    server = f"{scheme}://{u.hostname}:{u.port}"
    cfg: dict[str, str] = {"server": server}
    if u.username:
        cfg["username"] = u.username
        cfg["password"] = u.password or ""
    return cfg


async def try_playwright(query: str, proxy: str) -> dict[str, Any] | None:
    try:
        from playwright.async_api import async_playwright
    except Exception:
        return None

    try:
        async with async_playwright() as p:
            browser = await p.chromium.launch(
                headless=True,
                proxy=playwright_proxy_config(proxy),
            )
            context = await browser.new_context(
                user_agent=(
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                    "AppleWebKit/537.36 (KHTML, like Gecko) "
                    "Chrome/124.0.0.0 Safari/537.36"
                ),
                locale="id-ID",
            )
            page = await context.new_page()
            await page.goto(AHU_PROFIL_PT, wait_until="domcontentloaded", timeout=90000)
            await page.wait_for_selector("#nama", timeout=30000)
            for _ in range(40):
                ready = await page.evaluate("() => !!(window.grecaptcha && grecaptcha.execute)")
                if ready:
                    break
                await page.wait_for_timeout(500)
            else:
                await browser.close()
                return {"ok": False, "error": "playwright: grecaptcha not ready"}

            await page.fill("#nama", query)
            await page.evaluate(
                """() => {
                  if (window.jQuery) { jQuery('#admin-ubah-form').submit(); }
                  else {
                    const f = document.querySelector('#admin-ubah-form');
                    if (f) f.dispatchEvent(new Event('submit', {bubbles:true, cancelable:true}));
                  }
                }"""
            )
            try:
                await page.wait_for_url(re.compile(r"nama="), timeout=90000)
            except Exception:
                await page.wait_for_timeout(8000)
            await page.wait_for_timeout(1500)
            html = await page.content()
            await browser.close()

            parsed = parse_search_html(html, query)
            if not parsed:
                low = html.lower()
                if "tidak ditemukan" in low or "tidak ada" in low:
                    return {"ok": False, "error": "ahu not found"}
                return {
                    "ok": False,
                    "error": "playwright: captcha/search returned no cards",
                    "debug_excerpt": clean_text(html)[:400],
                }
            return {"ok": True, "result": parsed, "backend": "playwright"}
    except Exception as e:
        return {"ok": False, "error": f"playwright: {e}"}


async def try_crawl4ai(query: str, proxy: str) -> dict[str, Any] | None:
    try:
        from crawl4ai import AsyncWebCrawler, BrowserConfig, CrawlerRunConfig
    except Exception:
        return None

    js = f"""
    (async () => {{
      const sleep = (ms) => new Promise(r => setTimeout(r, ms));
      for (let i=0;i<40;i++) {{
        if (window.grecaptcha && grecaptcha.execute) break;
        await sleep(500);
      }}
      const input = document.querySelector('#nama');
      if (!input) return 'no-input';
      input.value = {json.dumps(query)};
      input.dispatchEvent(new Event('input', {{bubbles:true}}));
      if (window.jQuery) jQuery('#admin-ubah-form').submit();
      else {{
        const f = document.querySelector('#admin-ubah-form');
        if (f) f.dispatchEvent(new Event('submit', {{bubbles:true, cancelable:true}}));
      }}
      await sleep(8000);
      return 'ok';
    }})();
    """
    try:
        browser_cfg = BrowserConfig(
            headless=True,
            enable_stealth=True,
            verbose=False,
            proxy_config={"server": proxy} if proxy else None,
        )
        run_cfg = CrawlerRunConfig(
            wait_until="domcontentloaded",
            page_timeout=120000,
            delay_before_return_html=10.0,
            js_code=js,
            magic=True,
        )
        async with AsyncWebCrawler(config=browser_cfg) as crawler:
            result = await crawler.arun(url=AHU_PROFIL_PT, config=run_cfg)
            html = result.html if result and result.success else ""
            parsed = parse_search_html(html, query) if html else None
            if not parsed:
                return {
                    "ok": False,
                    "error": "crawl4ai: no result cards",
                    "debug_excerpt": clean_text(html)[:400] if html else "",
                }
            return {"ok": True, "result": parsed, "backend": "crawl4ai"}
    except Exception as e:
        return {"ok": False, "error": f"crawl4ai: {e}"}


async def try_scrapling(query: str, proxy: str) -> dict[str, Any] | None:
    try:
        from scrapling.fetchers import StealthyFetcher
    except Exception as e:
        return {"ok": False, "error": f"scrapling import: {e}"}

    def page_action(page):
        page.wait_for_selector("#nama", timeout=30000)
        for _ in range(40):
            ready = page.evaluate("() => !!(window.grecaptcha && grecaptcha.execute)")
            if ready:
                break
            page.wait_for_timeout(500)
        page.fill("#nama", query)
        page.evaluate(
            """() => {
              if (window.jQuery) jQuery('#admin-ubah-form').submit();
              else {
                const f = document.querySelector('#admin-ubah-form');
                if (f) f.dispatchEvent(new Event('submit', {bubbles:true, cancelable:true}));
              }
            }"""
        )
        page.wait_for_timeout(10000)

    try:
        kwargs: dict[str, Any] = {
            "headless": True,
            "solve_cloudflare": True,
            "timeout": 120000,
            "page_action": page_action,
        }
        if proxy:
            kwargs["proxy"] = proxy
        resp = await asyncio.to_thread(StealthyFetcher.fetch, AHU_PROFIL_PT, **kwargs)
        html = getattr(resp, "html", None) or getattr(resp, "body", None) or ""
        if isinstance(html, bytes):
            html = html.decode("utf-8", "ignore")
        parsed = parse_search_html(html, query)
        if not parsed:
            return {"ok": False, "error": "scrapling: no result cards", "debug_excerpt": clean_text(html)[:400]}
        return {"ok": True, "result": parsed, "backend": "scrapling"}
    except Exception as e:
        return {"ok": False, "error": f"scrapling: {e}"}


async def run_lookup(query: str, proxy: str, backend: str) -> dict[str, Any]:
    order = []
    if backend == "auto":
        order = [try_playwright, try_crawl4ai, try_scrapling]
    elif backend == "playwright":
        order = [try_playwright]
    elif backend == "crawl4ai":
        order = [try_crawl4ai]
    elif backend == "scrapling":
        order = [try_scrapling]

    last: dict[str, Any] = {"ok": False, "error": "ahu failed"}
    for fn in order:
        out = await fn(query, proxy)
        if out is None:
            continue
        last = out
        if out.get("ok"):
            result = out["result"]
            # Enrich with free transaction history
            hist = fetch_history(str(result.get("bakum_id") or ""), proxy)
            if hist:
                result["transactions"] = hist
            return {"ok": True, "result": result, "backend": out.get("backend")}
    return last


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--query", required=True)
    ap.add_argument(
        "--proxy",
        default=os.environ.get("AHU_PROXY")
        or os.environ.get("AHU_PROXY_URL")
        or os.environ.get("HTTPS_PROXY")
        or "",
    )
    ap.add_argument("--backend", choices=["auto", "playwright", "crawl4ai", "scrapling"], default="auto")
    args = ap.parse_args()
    if not args.proxy:
        return emit_obj({"ok": False, "error": "AHU_PROXY not set (use Clash mixed-port e.g. http://127.0.0.1:17890)"}, 2)

    out = asyncio.run(run_lookup(args.query.strip(), args.proxy.strip(), args.backend))
    if out.get("ok"):
        return emit_obj({"ok": True, "result": out.get("result"), "backend": out.get("backend")}, 0)
    return emit_obj(
        {
            "ok": False,
            "error": out.get("error"),
            "debug_excerpt": out.get("debug_excerpt"),
            "result": out.get("result"),
        },
        2,
    )


if __name__ == "__main__":
    sys.exit(main())
