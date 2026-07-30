#!/bin/bash
# ============================================================
# Google Maps 商家采集 —— 一键部署 + 自动更新脚本
# 用法：在服务器上执行一次，之后每次 push 到 main 自动部署
# ============================================================

set -e

PROJECT_DIR="/home/ubuntu/gmaps"
WEBHOOK_PORT=9000
WEBHOOK_SECRET="gmaps-auto-deploy-1d6eff2755f5986d"

echo "=============================================="
echo "  Google Maps 采集 —— 一键部署脚本"
echo "=============================================="
echo ""

# 1. 进入项目目录
echo "[1/5] 进入项目目录..."
cd $PROJECT_DIR

# 2. 拉取最新代码
echo "[2/5] 拉取最新代码..."
git pull origin main

# 3. 配置 Go 代理（国内加速）
echo "[3/5] 配置加速环境..."
cat > .env << 'EOF'
GOPROXY=https://goproxy.cn,direct
PLAYWRIGHT_DOWNLOAD_HOST=https://cdn.npmmirror.com/binaries/playwright
EOF

# 4. 构建并启动
echo "[4/5] 构建 Docker 镜像并启动（首次较慢，约 5-10 分钟）..."
sudo -E docker compose --env-file .env -f docker-compose.deploy.yaml up -d --build

echo ""
echo "=============================================="
echo "  部署完成！"
echo "=============================================="
echo ""
echo "访问地址: http://$(curl -s ifconfig.me):8080"
echo ""
echo "查看日志: sudo docker compose -f docker-compose.deploy.yaml logs -f"
echo "重启服务: sudo docker compose -f docker-compose.deploy.yaml restart"
echo ""

# 5. 设置自动部署（webhook 接收端）
echo "[5/5] 配置自动部署（可选）..."
read -p "是否配置 GitHub Webhook 自动部署？(y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo "正在配置 webhook 接收服务..."

    # 创建 webhook 处理脚本
    cat > /home/ubuntu/deploy-webhook.sh << 'DEPLOY_SCRIPT'
#!/bin/bash
cd /home/ubuntu/gmaps
git pull origin main
sudo -E docker compose --env-file .env -f docker-compose.deploy.yaml up -d --build
echo "$(date): 部署完成" >> /home/ubuntu/deploy.log
DEPLOY_SCRIPT

    chmod +x /home/ubuntu/deploy-webhook.sh

    # 用 socat 或者 python 起一个简单的 webhook 接收端
    if command -v python3 &> /dev/null; then
        cat > /home/ubuntu/webhook-server.py << 'PYEOF'
import http.server
import subprocess
import hashlib
import hmac
import json

SECRET = "WEBHOOK_SECRET_PLACEHOLDER"

class WebhookHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/webhook/deploy":
            self.send_response(404)
            self.end_headers()
            return

        content_length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(content_length)

        # 验证签名（可选，先注释掉方便调试）
        # signature = self.headers.get('X-Hub-Signature-256', '')
        # expected = 'sha256=' + hmac.new(SECRET.encode(), body, hashlib.sha256).hexdigest()
        # if not hmac.compare_digest(signature, expected):
        #     self.send_response(403)
        #     self.end_headers()
        #     return

        # 只处理 push 事件到 main 分支
        event = self.headers.get('X-GitHub-Event', '')
        if event == 'push':
            try:
                data = json.loads(body)
                ref = data.get('ref', '')
                if ref == 'refs/heads/main':
                    print(f"收到 main 分支 push，开始部署...")
                    subprocess.Popen(['/home/ubuntu/deploy-webhook.sh'])
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b'deploying')
                    return
            except:
                pass

        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'ok')

    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'ok')
            return
        self.send_response(404)
        self.end_headers()

if __name__ == '__main__':
    server = http.server.HTTPServer(('0.0.0.0', WEBHOOK_PORT_PLACEHOLDER), WebhookHandler)
    print(f'Webhook server running on port WEBHOOK_PORT_PLACEHOLDER')
    server.serve_forever()
PYEOF

        # 替换占位符
        sed -i "s/WEBHOOK_SECRET_PLACEHOLDER/$WEBHOOK_SECRET/g" /home/ubuntu/webhook-server.py
        sed -i "s/WEBHOOK_PORT_PLACEHOLDER/$WEBHOOK_PORT/g" /home/ubuntu/webhook-server.py

        # 创建 systemd 服务
        sudo bash -c "cat > /etc/systemd/system/gmaps-webhook.service << 'SVCEOF'
[Unit]
Description=GMaps Auto Deploy Webhook
After=network.target

[Service]
User=ubuntu
WorkingDirectory=/home/ubuntu
ExecStart=/usr/bin/python3 /home/ubuntu/webhook-server.py
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
SVCEOF"

        sudo systemctl daemon-reload
        sudo systemctl enable gmaps-webhook
        sudo systemctl start gmaps-webhook

        SERVER_IP=$(curl -s ifconfig.me)
        echo ""
        echo "=============================================="
        echo "  Webhook 配置完成！"
        echo "=============================================="
        echo ""
        echo "在 GitHub 仓库 Settings → Webhooks → Add webhook："
        echo "  Payload URL: http://$SERVER_IP:$WEBHOOK_PORT/webhook/deploy"
        echo "  Content type: application/json"
        echo "  Secret: $WEBHOOK_SECRET"
        echo "  Which events: Just the push event"
        echo ""
        echo " 测试: curl http://$SERVER_IP:$WEBHOOK_PORT/health"
        echo ""
    else
        echo "未找到 python3，跳过 webhook 配置"
    fi
fi

echo ""
echo "完成！"
