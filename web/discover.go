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

	renderJSON(w, http.StatusOK, res)
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
