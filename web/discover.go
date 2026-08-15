package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gosom/google-maps-scraper/engine"
)

func (s *Server) discoverPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/discover.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab == "" {
		tab = engine.KindPeople
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]string{"Tab": tab})
}

func (s *Server) outreachPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/outreach.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, nil)
}

func (s *Server) apiDiscoverSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	var q engine.Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "invalid json: " + err.Error(),
		})

		return
	}

	res, err := s.engine.Search(r.Context(), q)
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})

		return
	}

	scrubDiscoverResult(&res)

	renderJSON(w, http.StatusOK, res)
}

func scrubDiscoverResult(res *engine.Result) {
	if res == nil {
		return
	}

	res.Sources = []string{}
	res.Warnings = nil
	res.TookMS = 0
	if res.Note != "" {
		res.Note = "系统不会代发。"
	}
	for i := range res.Hits {
		res.Hits[i].Source = ""
	}
}

func (s *Server) apiDiscoverPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "url is required",
		})

		return
	}

	prev, err := s.engine.Preview(r.Context(), raw)
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})

		return
	}

	renderJSON(w, http.StatusOK, prev)
}

func (s *Server) apiDiscoverPreviewFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "url is required",
		})

		return
	}

	html, err := s.engine.PreviewFrameHTML(r.Context(), raw)
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})

		return
	}

	w.Header().Del("X-Frame-Options")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; img-src https: data:; style-src 'unsafe-inline' https:; font-src https: data:; frame-ancestors 'self'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (s *Server) apiDiscoverPlatforms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"platforms": engine.PeoplePlatformCatalog(),
	})
}

func (s *Server) apiDiscoverCountries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"countries": engine.SearchCountries,
	})
}

func (s *Server) apiDiscoverSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"people": []map[string]string{
			{
				"name":    "public-websearch",
				"stars":   "",
				"license": "",
				"pushed":  "",
				"use":     "默认：公开网页检索抽出抖音 / TikTok 及 Facebook / LinkedIn / Instagram / YouTube 等主页，无需 sidecar",
			},
			{
				"name":    "davidteather/TikTok-Api",
				"stars":   "6563",
				"license": "MIT",
				"pushed":  "2026-07",
				"use":     "TikTok 关键词搜人 / 主页（sidecar 增强）",
			},
			{
				"name":    "Johnserf-Seed/f2",
				"stars":   "2603",
				"license": "Apache-2.0",
				"pushed":  "2026-04",
				"use":     "TikTok 关键词搜作品抽作者；抖音主页 URL→资料",
			},
			{
				"name":    "s0md3v/Photon",
				"stars":   "13085",
				"license": "GPL-3.0（不链进 Go，只对齐其公开页 intel 抽取）",
				"pushed":  "2018+",
				"use":     "营销模式：公开检索定位页面后，抓取页面中的邮箱 / WhatsApp / 社媒链接",
			},
			{
				"name":    "sherlock-project/sherlock + soxoj/maigret",
				"stars":   "8万+/3.6万",
				"license": "MIT",
				"pushed":  "2026",
				"use":     "思路：同一账号去其他社媒找主页。不整站扫 400+ 站点（商家名不是用户名，误报高）。私信模式会从已找到的主页/官网抽链出社媒，并对拉丁账号探测 Instagram/TikTok/YouTube 等已支持平台",
			},
		},
		"skipped": []map[string]string{
			{
				"name":   "NanmiCoder/MediaCrawler",
				"stars":  "62389",
				"reason": "许可证为非商业学习许可，不能进商用产品",
			},
			{
				"name":   "Evil0ctal/Douyin_TikTok_Download_API",
				"stars":  "19352",
				"reason": "2025-10 后无主分支推送，且无关键词搜人接口",
			},
		},
	})
}
