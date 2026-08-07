"""Generate an xray client config from a vmess subscription.

Supports one or many nodes. Multiple nodes → one SOCKS inbound per node so
scrapemate can round-robin proxies and saturate concurrency.

Usage:
    SUB_URL=... NODE=Singapore python3 deploy/xray/gen_config.py deploy/xray/config.json
    SUB_URL=... MULTI=8 NODE_FILTER=新加坡,香港,日本,美国 python3 deploy/xray/gen_config.py ...

The generated file contains node credentials and must never be committed.
"""

from __future__ import annotations

import base64
import json
import os
import ssl
import sys
import urllib.request

SUB_URL = os.environ.get("SUB_URL", "")
NODE = os.environ.get("NODE", "")
MULTI = int(os.environ.get("MULTI", "1") or "1")
NODE_FILTER = os.environ.get("NODE_FILTER", "新加坡,香港,日本,美国,Taiwan,TW,JP,US,SG,HK")
SOCKS_PORT = int(os.environ.get("SOCKS_PORT", "1080"))
HTTP_PORT = int(os.environ.get("HTTP_PORT", "1081"))
SOCKS_USER = os.environ.get("SOCKS_USER", "")
SOCKS_PASS = os.environ.get("SOCKS_PASS", "")
# Extra SOCKS start at 1180 so we never collide with HTTP 1081
EXTRA_SOCKS_BASE = int(os.environ.get("EXTRA_SOCKS_BASE", "1180"))


def fetch_nodes(url):
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    req = urllib.request.Request(url, headers={"User-Agent": "v2rayN/6.0"})
    with urllib.request.urlopen(req, timeout=30, context=ctx) as resp:
        body = resp.read().decode()

    body += "=" * (-len(body) % 4)
    decoded = base64.b64decode(body).decode()

    nodes = []
    for line in decoded.split():
        if not line.startswith("vmess://"):
            continue
        raw = line[len("vmess://") :]
        raw += "=" * (-len(raw) % 4)
        try:
            nodes.append(json.loads(base64.b64decode(raw).decode()))
        except Exception:
            continue

    return nodes


def pick_one(nodes, name):
    if name:
        for node in nodes:
            if name.lower() in str(node.get("ps", "")).lower():
                return node
        raise SystemExit("no node matches " + name)
    return nodes[0]


def pick_many(nodes, n, filters, preferred_name=""):
    """Prefer preferred_name first, then filter keywords, then fill from rest."""
    chosen = []
    seen = set()

    def add(node):
        key = (node.get("add"), node.get("port"), node.get("id"), node.get("ps"))
        if key in seen:
            return
        seen.add(key)
        chosen.append(node)

    if preferred_name:
        for node in nodes:
            if preferred_name.lower() in str(node.get("ps", "")).lower():
                add(node)
                break

    keywords = [k.strip().lower() for k in filters.split(",") if k.strip()]
    for kw in keywords:
        for node in nodes:
            if len(chosen) >= n:
                break
            if kw in str(node.get("ps", "")).lower():
                add(node)
        if len(chosen) >= n:
            break

    for node in nodes:
        if len(chosen) >= n:
            break
        add(node)

    if not chosen:
        raise SystemExit("no nodes available from subscription")
    return chosen[:n]


def socks_settings():
    if SOCKS_USER and SOCKS_PASS:
        return {
            "udp": True,
            "auth": "password",
            "accounts": [{"user": SOCKS_USER, "pass": SOCKS_PASS}],
        }
    return {"udp": True, "auth": "noauth"}


def http_settings():
    if SOCKS_USER and SOCKS_PASS:
        return {"accounts": [{"user": SOCKS_USER, "pass": SOCKS_PASS}]}
    return {}


def outbound_for(node, tag):
    network = node.get("net", "tcp") or "tcp"
    stream = {
        "network": network,
        "security": "tls" if node.get("tls") == "tls" else "none",
    }
    if network == "ws":
        stream["wsSettings"] = {
            "path": node.get("path") or "/",
            "headers": {"Host": node.get("host") or node["add"]},
        }
    elif network == "tcp" and node.get("type") == "http":
        stream["tcpSettings"] = {
            "header": {
                "type": "http",
                "request": {
                    "path": [node.get("path") or "/"],
                    "headers": {"Host": [node.get("host") or node["add"]]},
                },
            }
        }

    return {
        "tag": tag,
        "protocol": "vmess",
        "settings": {
            "vnext": [
                {
                    "address": node["add"],
                    "port": int(node["port"]),
                    "users": [
                        {
                            "id": node["id"],
                            "alterId": int(node.get("aid") or 0),
                            "security": node.get("scy") or "auto",
                        }
                    ],
                }
            ]
        },
        "streamSettings": stream,
    }


def socks_ports_for(count):
    ports = [SOCKS_PORT]
    for i in range(1, count):
        ports.append(EXTRA_SOCKS_BASE + i - 1)
    return ports


def build(nodes):
    inbounds = []
    outbounds = []
    rules = []
    ports = socks_ports_for(len(nodes))

    for i, node in enumerate(nodes):
        otag = f"proxy-{i}"
        itag = f"socks-{i}"
        outbounds.append(outbound_for(node, otag))
        inbounds.append(
            {
                "tag": itag,
                "listen": "0.0.0.0",
                "port": ports[i],
                "protocol": "socks",
                "settings": socks_settings(),
            }
        )
        rules.append({"type": "field", "inboundTag": [itag], "outboundTag": otag})

    # Keep HTTP inbound on first node for debugging
    inbounds.append(
        {
            "tag": "http",
            "listen": "0.0.0.0",
            "port": HTTP_PORT,
            "protocol": "http",
            "settings": http_settings(),
        }
    )
    rules.append({"type": "field", "inboundTag": ["http"], "outboundTag": "proxy-0"})

    outbounds.append({"tag": "direct", "protocol": "freedom"})
    outbounds.append({"tag": "block", "protocol": "blackhole"})

    return {
        "log": {"loglevel": "warning"},
        "inbounds": inbounds,
        "outbounds": outbounds,
        "routing": {"domainStrategy": "AsIs", "rules": rules},
    }, ports


def main():
    if not SUB_URL:
        raise SystemExit("SUB_URL is required")

    out = sys.argv[1] if len(sys.argv) > 1 else "deploy/xray/config.json"
    nodes = fetch_nodes(SUB_URL)
    if not nodes:
        raise SystemExit("subscription returned no vmess nodes")

    if MULTI > 1:
        selected = pick_many(nodes, MULTI, NODE_FILTER, NODE)
    else:
        selected = [pick_one(nodes, NODE)]

    cfg, ports = build(selected)
    with open(out, "w") as fh:
        json.dump(cfg, fh, indent=2, ensure_ascii=False)

    # Machine-readable ports list for deploy.sh
    ports_path = out + ".ports"
    with open(ports_path, "w") as fh:
        fh.write(",".join(str(p) for p in ports))

    print("using nodes:")
    for i, node in enumerate(selected):
        print(f"  [{ports[i]}] {node.get('ps')}")
    print("ports_file:", ports_path)


if __name__ == "__main__":
    main()
