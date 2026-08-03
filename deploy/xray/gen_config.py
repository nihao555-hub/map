"""Generate an xray client config from a vmess subscription.

Usage:
    SUB_URL=... NODE=Singapore python3 deploy/xray/gen_config.py deploy/xray/config.json

The generated file contains node credentials and must never be committed.
"""

import base64
import json
import os
import ssl
import sys
import urllib.request

SUB_URL = os.environ.get("SUB_URL", "")
NODE = os.environ.get("NODE", "")
SOCKS_PORT = int(os.environ.get("SOCKS_PORT", "1080"))
HTTP_PORT = int(os.environ.get("HTTP_PORT", "1081"))
SOCKS_USER = os.environ.get("SOCKS_USER", "")
SOCKS_PASS = os.environ.get("SOCKS_PASS", "")


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
        raw = line[len("vmess://"):]
        raw += "=" * (-len(raw) % 4)
        nodes.append(json.loads(base64.b64decode(raw).decode()))

    return nodes


def pick(nodes, name):
    if name:
        for node in nodes:
            if name.lower() in str(node.get("ps", "")).lower():
                return node
        raise SystemExit("no node matches " + name)

    return nodes[0]


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


def build(node):
    return {
        "log": {"loglevel": "warning"},
        "inbounds": [
            {
                "tag": "socks",
                "listen": "0.0.0.0",
                "port": SOCKS_PORT,
                "protocol": "socks",
                "settings": socks_settings(),
            },
            {
                "tag": "http",
                "listen": "0.0.0.0",
                "port": HTTP_PORT,
                "protocol": "http",
                "settings": http_settings(),
            },
        ],
        "outbounds": [
            {
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
                "streamSettings": {
                    "network": node.get("net", "tcp"),
                    "security": "tls" if node.get("tls") == "tls" else "none",
                    "wsSettings": {
                        "path": node.get("path") or "/",
                        "headers": {"Host": node.get("host") or node["add"]},
                    },
                },
            }
        ],
    }


def main():
    if not SUB_URL:
        raise SystemExit("SUB_URL is required")

    out = sys.argv[1] if len(sys.argv) > 1 else "deploy/xray/config.json"
    node = pick(fetch_nodes(SUB_URL), NODE)

    with open(out, "w") as fh:
        json.dump(build(node), fh, indent=2)

    print("using node:", node.get("ps"))


if __name__ == "__main__":
    main()
