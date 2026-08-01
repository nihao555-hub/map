package web

import (
	"fmt"
	"net/url"
	"strings"
)

// 抓取器要求代理必须带用户名密码认证（否则任务跑到一半才在 auth proxy 处失败），
// 且仅支持这几种协议（与 scrapemate.NewProxy 保持一致）
var supportedProxySchemes = []string{"http", "https", "socks5", "socks5h"}

// validateProxyLines 校验代理文本框（每行一个代理）的内容。
// 空行和首尾空格被忽略，行号按原始文本行计算，方便用户定位。
// 返回清洗后的代理列表；只要有一行非法就返回中文错误并拒绝提交。
func validateProxyLines(raw string) ([]string, error) {
	var proxies []string

	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if err := validateProxy(line); err != nil {
			return nil, fmt.Errorf("第 %d 行代理%w", i+1, err)
		}

		proxies = append(proxies, line)
	}

	return proxies, nil
}

// validateProxy 校验单行代理 URL：协议受支持、有主机、带用户名密码认证
func validateProxy(p string) error {
	// 与 scrapemate.NewProxy 行为一致：没有 scheme 时按 socks5 处理
	u := p
	if !strings.Contains(u, "://") {
		u = "socks5://" + u
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("格式非法：%v", err)
	}

	scheme := strings.ToLower(parsed.Scheme)

	valid := false

	for _, s := range supportedProxySchemes {
		if scheme == s {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("协议 %q 不支持，仅支持 http/https/socks5/socks5h", parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("缺少主机地址，格式应为 http://用户:密码@主机:端口")
	}

	if parsed.User == nil {
		return fmt.Errorf("缺少用户名密码认证，格式应为 http://用户:密码@主机:端口")
	}

	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()

	if username == "" || password == "" || !hasPassword {
		return fmt.Errorf("缺少用户名密码认证，格式应为 http://用户:密码@主机:端口")
	}

	return nil
}
