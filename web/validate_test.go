//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"strings"
	"testing"
)

func TestValidateProxyLines(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string // 错误信息中应包含的片段，空表示不应报错
	}{
		{
			name: "空输入",
			raw:  "",
			want: nil,
		},
		{
			name: "只有空行和空白",
			raw:  "\n  \n\t\n",
			want: nil,
		},
		{
			name: "合法 http 带认证",
			raw:  "http://user:pass@1.2.3.4:8080",
			want: []string{"http://user:pass@1.2.3.4:8080"},
		},
		{
			name: "合法 https 带认证",
			raw:  "https://user:pass@proxy.example.com:8443",
			want: []string{"https://user:pass@proxy.example.com:8443"},
		},
		{
			name: "合法 socks5 带认证",
			raw:  "socks5://user:pass@1.2.3.4:1080",
			want: []string{"socks5://user:pass@1.2.3.4:1080"},
		},
		{
			name: "无 scheme 按 socks5 处理",
			raw:  "user:pass@1.2.3.4:1080",
			want: []string{"user:pass@1.2.3.4:1080"},
		},
		{
			name: "首尾空格被裁剪",
			raw:  "  http://user:pass@1.2.3.4:8080  ",
			want: []string{"http://user:pass@1.2.3.4:8080"},
		},
		{
			name: "多行混合空行",
			raw:  "http://u1:p1@1.1.1.1:80\n\nsocks5://u2:p2@2.2.2.2:1080\n",
			want: []string{"http://u1:p1@1.1.1.1:80", "socks5://u2:p2@2.2.2.2:1080"},
		},
		{
			name:    "缺少用户名密码",
			raw:     "http://1.2.3.4:8080",
			wantErr: "缺少用户名密码认证",
		},
		{
			name:    "只有用户名没有密码",
			raw:     "http://user@1.2.3.4:8080",
			wantErr: "缺少用户名密码认证",
		},
		{
			name:    "密码为空",
			raw:     "http://user:@1.2.3.4:8080",
			wantErr: "缺少用户名密码认证",
		},
		{
			name:    "错误行号定位到第二行",
			raw:     "http://u:p@1.1.1.1:80\nhttp://2.2.2.2:8080",
			wantErr: "第 2 行代理缺少用户名密码认证",
		},
		{
			name:    "错误行号跳过空行",
			raw:     "\n\nhttp://2.2.2.2:8080",
			wantErr: "第 3 行代理缺少用户名密码认证",
		},
		{
			name:    "不支持的协议",
			raw:     "ftp://user:pass@1.2.3.4:21",
			wantErr: "不支持",
		},
		{
			name:    "畸形 URL 缺少主机",
			raw:     "http://user:pass@",
			wantErr: "缺少主机地址",
		},
		{
			name:    "畸形 URL 非法字符",
			raw:     "http://user:pass@ho st:8080",
			wantErr: "第 1 行代理",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateProxyLines(tt.raw)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d proxies, got %d (%v)", len(tt.want), len(got), got)
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("proxy %d: expected %q, got %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}
