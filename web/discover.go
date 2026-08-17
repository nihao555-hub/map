package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/engine"
)

func (s *Server) directoryPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tmpl, ok := s.tmpl["static/templates/directory.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, nil)
}

func (s *Server) apiDiscoverDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	page, err := s.engine.BrowseDirectory(r.Context(), engine.DirectoryBrowseQuery{
		Filter:  strings.TrimSpace(r.URL.Query().Get("filter")),
		Source:  strings.TrimSpace(r.URL.Query().Get("source")),
		Country: strings.TrimSpace(r.URL.Query().Get("country")),
		Name:    strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	renderJSON(w, http.StatusOK, page)
}

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

func (s *Server) customsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/customs.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, nil)
}

func (s *Server) exhibitionPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/exhibition.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, nil)
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
	switch res.Kind {
	case engine.KindCustoms:
		if res.Note == "" {
			res.Note = "系统不会代发。逐票企业来自美国海关公开提单，不是全球企业库。"
		}
	case engine.KindExhibition:
		res.Note = "系统不会代发。公开参展商名单来自展会官网和公开名录，不是官方全量库。"
	default:
		res.Note = "系统不会代发。中文品类会译成当地采购词，按所选国家找进口商、经销商和工程商公开主页。不是外贸通那种一次几万条的企业库。"
	}
	for i := range res.Hits {
		res.Hits[i].Source = ""
		if res.Hits[i].Extra == nil {
			continue
		}
		delete(res.Hits[i].Extra, "q")
		if len(res.Hits[i].Extra) == 0 {
			res.Hits[i].Extra = nil
		}
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
				"name":    "Wikidata / QLever (P7085 / P7120)",
				"stars":   "公开知识库",
				"license": "CC0",
				"pushed":  "持续",
				"use":     "抖音/TikTok 唯一能一次拉全的已标注号：TikTok 非人名约 14410，抖音约 182。高 star 爬虫都没有全库数据包",
			},
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
				"use":     "TikTok 关键词搜人 / 主页（sidecar 增强，要 Chromium + 可选 msToken；是抽样不是全库）",
			},
			{
				"name":    "Johnserf-Seed/f2",
				"stars":   "2603",
				"license": "Apache-2.0",
				"pushed":  "2026-04",
				"use":     "TikTok 关键词搜作品抽作者；抖音主页 URL→资料。搜用户仍为上游 🔵",
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
				"stars":   "8.9万/3.6万",
				"license": "MIT",
				"pushed":  "2026",
				"use":     "Sherlock 站点表：官网域名和已有社媒 handle 探姐妹主页；足够独特且全球唯一的法律名还要能对上 LinkedIn 公司页或两个核心平台。不拿短名/重名去撞 400 站",
			},
			{
				"name":    "laramies/theHarvester + OpenStreetMap Overpass",
				"stars":   "13k / OSM",
				"license": "GPL-2.0 / ODbL",
				"pushed":  "2026",
				"use":     "全库缺社媒后台补：官网 HTML、OSM contact:*、店名公开检索 Facebook/Instagram/LinkedIn（theHarvester 同源），再对已有 handle 走 Sherlock 姐妹页；搜索时也对当前页缺社媒的卡片即时补",
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
				"reason": "只解析已有主页/作品，没有关键词搜人，也没有全量企业号包",
			},
			{
				"name":   "drawrowfly/tiktok-scraper",
				"stars":  "5052",
				"reason": "2023 停更，2026 已基本不可用",
			},
			{
				"name":   "Common Crawl CDX",
				"stars":  "官方索引",
				"reason": "最新库 CC-MAIN-2026-30 里 tiktok.com/@ 为 0 页，抖音 user 也几乎没有；平台拦了爬虫",
			},
		},
	})
}

func (s *Server) apiCustomsProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "请输入企业名称",
		})

		return
	}

	year, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("year")))
	pageURL := strings.TrimSpace(r.URL.Query().Get("url"))
	prof, err := s.engine.LookupCustomsProfileAt(r.Context(), name, pageURL, year)
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})

		return
	}

	if strings.Contains(strings.ToLower(prof.Note), "kirchner") {
		prof.Note = "系统不会代发。逐票企业来自美国海关公开提单，不是全球企业库。"
	}

	renderJSON(w, http.StatusOK, prof)
}

func (s *Server) apiExhibitionExhibitors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})
		return
	}

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	pageURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if name == "" && pageURL == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "请输入展会名称",
		})
		return
	}

	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	out, err := s.engine.LookupFairExhibitors(r.Context(), name, pageURL, limit)
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	out.Note = "系统不会代发。公开参展商名单来自展会官网和公开名录，不是官方全量库。"
	renderJSON(w, http.StatusOK, out)
}
