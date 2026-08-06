package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// jsonJS 把值编成可安全嵌入 <script> 的 JSON（带引号的字符串/数组/对象）
func jsonJS(v any) (template.JS, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}

	return template.JS(b), nil
}

//go:embed static
var static embed.FS

type Server struct {
	tmpl       map[string]*template.Template
	srv        *http.Server
	svc        *Service
	invites    InviteStore
	inviteGate bool
}

// ServerOption configures optional Server behavior.
type ServerOption func(*Server)

// WithInvite enables the invite-code gate using the given store.
func WithInvite(store InviteStore, enabled bool) ServerOption {
	return func(s *Server) {
		s.invites = store
		s.inviteGate = enabled && store != nil
	}
}

func New(svc *Service, addr string, opts ...ServerOption) (*Server, error) {
	ans := Server{
		svc:  svc,
		tmpl: make(map[string]*template.Template),
		srv: &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}
	for _, opt := range opts {
		opt(&ans)
	}

	staticFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(staticFS))
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	// Doubao-style Agent SPA (Vite build → static/agent), served at /agent/
	mux.HandleFunc("/agent", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/agent/", http.StatusFound)
	})
	mux.Handle("/agent/", http.HandlerFunc(ans.serveAgentApp))
	mux.HandleFunc("/invite", ans.invitePage)
	mux.HandleFunc("/scrape", ans.scrape)
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		ans.download(w, r)
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		ans.delete(w, r)
	})
	mux.HandleFunc("/jobs", ans.getJobs)
	mux.HandleFunc("/view", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		ans.viewJob(w, r)
	})
	mux.HandleFunc("/", ans.index)

	// api routes
	mux.HandleFunc("/api/docs", ans.redocHandler)
	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			ans.apiScrape(w, r)
		case http.MethodGet:
			ans.apiGetJobs(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		switch r.Method {
		case http.MethodGet:
			ans.apiGetJob(w, r)
		case http.MethodDelete:
			ans.apiDeleteJob(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/jobs/{id}/places", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)

			return
		}

		ans.apiGetPlaces(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/places/count", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		if r.Method != http.MethodGet {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
			return
		}
		ans.apiGetPlacesCount(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
			return
		}
		ans.apiCancelJob(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/queue", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.apiJobQueue(w, r)
	})
	mux.HandleFunc("/api/v1/agent/jobs/queue", ans.apiAgentJobsQueue)

	mux.HandleFunc("/cancel", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		ans.cancelJob(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/places/{place_id}/intel", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			})
			return
		}
		ans.apiPlaceIntel(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/intel/status", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)
		if r.Method != http.MethodGet {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
			return
		}
		ans.apiJobIntelStatus(w, r)
	})

	mux.HandleFunc("/api/v1/osint-status", ans.apiOSINTStatus)
	mux.HandleFunc("/api/v1/media", ans.apiMediaProxy)

	mux.HandleFunc("/api/v1/geocode", ans.apiGeocode)
	mux.HandleFunc("/api/v1/reverse-geocode", ans.apiReverseGeocode)
	mux.HandleFunc("/api/v1/ai-translate", ans.apiAITranslate)
	mux.HandleFunc("/api/v1/ai-status", ans.apiAIStatus)
	mux.HandleFunc("/api/v1/agent/understand", ans.apiAgentUnderstand)
	mux.HandleFunc("/api/v1/agent/dispatch", ans.apiAgentDispatch)
	mux.HandleFunc("/api/v1/agent/dispatch/stream", ans.apiAgentDispatchStream)
	mux.HandleFunc("/api/v1/system/concurrency", ans.apiConcurrency)

	mux.HandleFunc("/api/v1/jobs/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}

			renderJSON(w, http.StatusMethodNotAllowed, ans)

			return
		}

		ans.download(w, r)
	})

	handler := securityHeaders(ans.inviteGateMiddleware(mux))
	ans.srv.Handler = handler

	tmplsKeys := []string{
		"static/templates/index.html",
		"static/templates/job_rows.html",
		"static/templates/job_row.html",
		"static/templates/job_view.html",
		"static/templates/redoc.html",
		"static/templates/invite.html",
	}

	for _, key := range tmplsKeys {
		tmp, err := template.ParseFS(static, key)
		if err != nil {
			return nil, err
		}

		ans.tmpl[key] = tmp
	}

	return &ans, nil
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()

		err := s.srv.Shutdown(context.Background())
		if err != nil {
			log.Println(err)

			return
		}

		log.Println("server stopped")
	}()

	fmt.Fprintf(os.Stderr, "visit http://localhost%s\n", s.srv.Addr)

	err := s.srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

type formData struct {
	Name      string
	MaxTime   string
	Keywords  []string
	Locations string
	Language  string
	Zoom      int
	FastMode  bool
	Radius    int
	Lat       string
	Lon       string
	Depth     int
	Email     bool
	Proxies   []string
}

type ctxKey string

const idCtxKey ctxKey = "id"

func requestWithID(r *http.Request) *http.Request {
	id := r.PathValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}

	parsed, err := uuid.Parse(id)
	if err == nil {
		r = r.WithContext(context.WithValue(r.Context(), idCtxKey, parsed))
	}

	return r
}

func getIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(idCtxKey).(uuid.UUID)

	return id, ok
}

//nolint:gocritic // this is used in template
func (f formData) ProxiesString() string {
	return strings.Join(f.Proxies, "\n")
}

//nolint:gocritic // this is used in template
func (f formData) KeywordsString() string {
	return strings.Join(f.Keywords, "\n")
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/index.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	data := formData{
		Name:     "",
		MaxTime:  "10m",
		Keywords: []string{},
		Language: "en",
		Zoom:     15,
		FastMode: false,
		Radius:   10000,
		Lat:      "0",
		Lon:      "0",
		Depth:    10,
		Email:    false,
	}

	_ = tmpl.Execute(w, data)
}

// serveAgentApp serves the React Agent workspace (ai-elements UI) built into static/agent.
// SPA fallback: unknown paths under /agent/ return index.html.
func (s *Server) serveAgentApp(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/agent/")
	if rel == "" || strings.HasSuffix(rel, "/") {
		rel = "index.html"
	}
	// Prevent path traversal
	rel = path.Clean("/" + rel)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "." || rel == "" {
		rel = "index.html"
	}

	data, err := static.ReadFile("static/agent/" + rel)
	if err != nil {
		// SPA fallback for client routes
		data, err = static.ReadFile("static/agent/index.html")
		if err != nil {
			http.Error(w, "agent UI not built — run: cd web/agent-ui && npm run build", http.StatusNotFound)
			return
		}
		rel = "index.html"
	}

	ctype := "text/html; charset=utf-8"
	switch {
	case strings.HasSuffix(rel, ".js"):
		ctype = "application/javascript; charset=utf-8"
	case strings.HasSuffix(rel, ".css"):
		ctype = "text/css; charset=utf-8"
	case strings.HasSuffix(rel, ".svg"):
		ctype = "image/svg+xml"
	case strings.HasSuffix(rel, ".png"):
		ctype = "image/png"
	case strings.HasSuffix(rel, ".woff2"):
		ctype = "font/woff2"
	case strings.HasSuffix(rel, ".json"):
		ctype = "application/json"
	case strings.HasSuffix(rel, ".map"):
		ctype = "application/json"
	}
	w.Header().Set("Content-Type", ctype)
	if rel != "index.html" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(data)
}

func (s *Server) scrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   strings.TrimSpace(r.Form.Get("name")),
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   JobData{},
	}

	maxTimeStr := r.Form.Get("maxtime")

	maxTime, err := time.ParseDuration(maxTimeStr)
	if err != nil {
		http.Error(w, "invalid max time", http.StatusUnprocessableEntity)

		return
	}

	if maxTime < time.Minute*3 {
		http.Error(w, "max time must be more than 3m", http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.MaxTime = maxTime

	keywordsStr, ok := r.Form["keywords"]
	if !ok {
		http.Error(w, "missing keywords", http.StatusUnprocessableEntity)

		return
	}

	locationsStr := r.Form.Get("locations")
	locationsStr = strings.TrimSpace(locationsStr)

	// 先保留用户原始中文关键词；海外搜索时再译成当地语言拼进 Maps 查询
	var rawKeywords []string
	for _, k := range strings.Split(keywordsStr[0], "\n") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		rawKeywords = append(rawKeywords, k)
	}

	newJob.Data.Lang = r.Form.Get("lang")

	newJob.Data.Zoom, err = strconv.Atoi(r.Form.Get("zoom"))
	if err != nil {
		http.Error(w, "invalid zoom", http.StatusUnprocessableEntity)

		return
	}

	// 目标半径：优先 radius_km（公里，项目上限 MaxRadiusKm），否则兼容旧 radius（米）
	newJob.Data.Radius, err = parseTargetRadiusMeters(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.Lat = r.Form.Get("latitude")
	newJob.Data.Lon = r.Form.Get("longitude")

	newJob.Data.Depth, err = strconv.Atoi(r.Form.Get("depth"))
	if err != nil || newJob.Data.Depth <= 0 {
		newJob.Data.Depth = 50
	}

	// 背调：用户抓取前勾选；开启后结果照常产出，背调并发进行
	newJob.Data.EnableIntel = r.Form.Get("enable_intel") == "on" ||
		r.Form.Get("enable_intel") == "true" || r.Form.Get("enable_intel") == "1"

	// 界面/AI 产出语言（与 Maps hl/lang 独立）；缺省英文
	newJob.Data.UILang = normalizeUILang(r.Form.Get("ui_lang"))

	// 结果列：深度全量默认全列
	newJob.Data.Columns = strings.TrimSpace(r.Form.Get("columns"))
	if bbox := r.Form.Get("gridbbox"); bbox != "" {
		newJob.Data.GridBBox = bbox
	}
	if cellStr := r.Form.Get("gridcell"); cellStr != "" {
		if v, err := strconv.ParseFloat(cellStr, 64); err == nil && v > 0 {
			newJob.Data.GridCellKm = v
		}
	}
	newJob.Data.Locations = locationsStr

	// 产品策略：一律深度 + 半径内网格全量，不设数量上限；代理只用服务器 GMS_PROXIES
	ApplyFullVolumeDefaults(&newJob.Data, newJob.Data.Radius)
	log.Printf("深度全量模式 cell=%.1fkm radius=%dm (%.1fkm) unlimited depth=%d",
		newJob.Data.GridCellKm, newJob.Data.Radius, float64(newJob.Data.Radius)/1000, newJob.Data.Depth)

	// 用户显式选择的目标国家（优先于地理编码推断）
	countryCode := strings.ToLower(strings.TrimSpace(r.Form.Get("country_code")))
	countryName := strings.TrimSpace(r.Form.Get("country_name"))
	useAI := r.Form.Get("ai_translate") == "on" || r.Form.Get("ai_translate") == "true"
	if useAI && !AITranslateEnabled() {
		log.Printf("已勾选 AI 翻译但未配置 GRSAI_API_KEY，将回退词典/机翻")
		useAI = false
	}
	newJob.Data.CountryCode = countryCode
	newJob.Data.CountryName = countryName
	newJob.Data.RawKeywords = append([]string(nil), rawKeywords...)
	if countryCode != "" {
		if hl := langForCountryCode(countryCode); hl != "" {
			newJob.Data.Lang = hl
		}
	}

	// 地理锚定 + 海外中文查询本地化：
	// 1) 锚定经纬度 / 按国家校正 hl
	// 2) 把「咖啡 in 纽约」译成「coffee in New York」再交给 Google Maps
	searchLocation := locationsStr
	// 前端已锚定 lat/lon 且选定国家时，跳过二次地理编码（可省数秒）
	skipGeocode := hasGeoAnchor(newJob.Data.Lat, newJob.Data.Lon) && countryCode != ""
	if locationsStr != "" && !skipGeocode {
		geoCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)

		point, geoErr := ResolveLocationAnchor(geoCtx, locationsStr, countryCode)

		cancel()

		if geoErr != nil {
			log.Printf("地理编码 %q 失败: %v，回退为未锚定搜索", locationsStr, geoErr)
		} else {
			if !hasGeoAnchor(newJob.Data.Lat, newJob.Data.Lon) {
				newJob.Data.Lat = strconv.FormatFloat(point.Lat, 'f', 6, 64)
				newJob.Data.Lon = strconv.FormatFloat(point.Lon, 'f', 6, 64)
				log.Printf("地点 %q 锚定到 %s,%s", locationsStr, newJob.Data.Lat, newJob.Data.Lon)
			}

			// 未选手动国家时，才用地理编码结果校正语言
			if countryCode == "" {
				if hl := langForCountryCode(point.CountryCode); hl != "" && hl != newJob.Data.Lang {
					log.Printf("地点 %q 国家代码 %s，hl 从 %s 调整为 %s",
						locationsStr, point.CountryCode, newJob.Data.Lang, hl)

					newJob.Data.Lang = hl
				}
			}

			// 海外中文地名：优先词典/英文展示名，避免 Maps 吃中文地点
			if containsChinese(locationsStr) && newJob.Data.Lang != "zh" {
				if loc, ok := zhPlaceLexicon[locationsStr]; ok {
					searchLocation = loc
				} else if short := shortDisplayName(point.DisplayName); short != "" && !containsChinese(short) {
					searchLocation = short
				}
			}
		}
	} else if locationsStr != "" && skipGeocode {
		if containsChinese(locationsStr) && newJob.Data.Lang != "zh" {
			if loc, ok := zhPlaceLexicon[locationsStr]; ok {
				searchLocation = loc
			}
		}
		// 地图点选常把「在哪里」写成坐标：不要把坐标拼进 Maps 查询词
		if isLatLonLocation(searchLocation) {
			if countryName != "" {
				searchLocation = countryName
			} else {
				searchLocation = ""
			}
		}
		log.Printf("跳过地理编码：已有锚点 %s,%s country=%s searchLoc=%q", newJob.Data.Lat, newJob.Data.Lon, countryCode, searchLocation)
	}

	// 任务名始终由服务端用当前关键词+地点生成，避免前端隐藏域残留导致「名实不符」
	newJob.Name = buildJobName(rawKeywords, locationsStr)

	// 关键词本地化：词典优先；仅未命中时短超时走 AI
	{
		locTimeout := 6 * time.Second
		if useAI && AITranslateEnabled() {
			locTimeout = 10 * time.Second
		}
		locCtx, cancel := context.WithTimeout(r.Context(), locTimeout)
		localized, locUsed, did := localizeSearchQuery(locCtx, rawKeywords, searchLocation, newJob.Data.Lang, localizeOpts{
			CountryName: countryName,
			UseAI:       useAI,
		})
		cancel()

		if len(localized) == 0 {
			http.Error(w, "无法将中文关键词译成目标国可搜词（机翻不可用）。请改用英文/当地语言品类，或勾选 AI 翻译后重试", http.StatusUnprocessableEntity)

			return
		}

		// 二次保险：海外任务绝不带汉字进 Google Maps（否则常只命中 1～2 家无关店）
		if newJob.Data.Lang != "zh" {
			clean := make([]string, 0, len(localized))
			for _, kw := range localized {
				if containsChinese(kw) {
					log.Printf("丢弃仍含中文的查询: %q", kw)
					continue
				}
				clean = append(clean, kw)
			}
			if len(clean) == 0 {
				http.Error(w, "关键词仍含中文，无法在目标国 Google Maps 有效搜索。请填写英文品类（如 importer / cafe）或勾选 AI 翻译", http.StatusUnprocessableEntity)

				return
			}
			localized = clean
		}

		newJob.Data.Keywords = localized
		if did {
			log.Printf("中文查询已本地化: name=%q lang=%s country=%s ai=%v loc=%q -> %v",
				newJob.Name, newJob.Data.Lang, countryCode, useAI && AITranslateEnabled(), locUsed, localized)
		}
	}

	if newJob.Data.Locations == "" && locationsStr != "" {
		newJob.Data.Locations = locationsStr
	}

	// 必填：关键词 +（国家或地点）+ 半径（已在上方解析）
	if len(rawKeywords) == 0 {
		http.Error(w, "请填写「找什么」（必填）", http.StatusUnprocessableEntity)
		return
	}
	if countryCode == "" && locationsStr == "" && !hasGeoAnchor(newJob.Data.Lat, newJob.Data.Lon) {
		http.Error(w, "请填写「目标国家」和「在哪里」（必填），或在地图上选点", http.StatusUnprocessableEntity)
		return
	}

	// 代理仅使用服务器配置（GMS_PROXIES / -proxies），不再接受用户表单代理
	newJob.Data.Proxies = nil

	err = newJob.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)

		return
	}

	log.Printf("任务意图 name=%q raw=%v search=%v country=%s(%s) lang=%s loc=%q geo=%s,%s deep+grid unlimited radius=%dm",
		newJob.Name, newJob.Data.RawKeywords, newJob.Data.Keywords,
		newJob.Data.CountryName, newJob.Data.CountryCode, newJob.Data.Lang,
		newJob.Data.Locations, newJob.Data.Lat, newJob.Data.Lon, newJob.Data.Radius)

	if err := s.attachOwner(r, &newJob); err != nil {
		http.Error(w, "需要有效邀请会话", http.StatusUnauthorized)
		return
	}

	err = s.svc.Create(r.Context(), &newJob)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	tmpl, ok := s.tmpl["static/templates/job_row.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, newJob)
}

func (s *Server) getJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	tmpl, ok := s.tmpl["static/templates/job_rows.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)
		return
	}

	jobs, err := s.listJobsForRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, jobs)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ctx := r.Context()

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	job, err := s.loadAccessibleJob(r, id.String())
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	filePath, err := s.svc.GetCSV(ctx, id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	jobName := job.Name

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	st, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to stat file", http.StatusInternalServerError)
		return
	}

	fileName := csvDownloadFilename(jobName, id.String())
	w.Header().Set("Content-Disposition", contentDispositionAttachment(fileName))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	if r.Method == http.MethodHead {
		// Approximate size (+ optional BOM). Exact size not critical for HEAD.
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size()+3, 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	// Excel on Windows often misreads UTF-8 CSV without BOM.
	bom := make([]byte, 3)
	n, _ := file.Read(bom)
	hasBOM := n >= 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}
	if !hasBOM {
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return
		}
	}

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "Failed to send file", http.StatusInternalServerError)
		return
	}
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	deleteID, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	if _, err := s.loadAccessibleJob(r, deleteID.String()); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	err := s.svc.Delete(r.Context(), deleteID.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiScrapeRequest struct {
	Name string
	JobData
}

type apiScrapeResponse struct {
	ID string `json:"id"`
}

func (s *Server) redocHandler(w http.ResponseWriter, _ *http.Request) {
	tmpl, ok := s.tmpl["static/templates/redoc.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, nil)
}

// apiAIStatus 告诉前端 AI 翻译是否已配置（不暴露密钥）
func (s *Server) apiAIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"enabled": AITranslateEnabled(),
		"model":   grsaiModel(),
		"host":    grsaiHost(),
		"note":    "IntentAgent / 翻译 / 背调均走 GRSAI（OpenAI 兼容）。未配置 GRSAI_API_KEY 时 Intent 回退规则引擎。",
	})
}

// apiAITranslate 用配置的 Gemini 兼容接口把中文关键词译成目标国搜索词
func (s *Server) apiAITranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})

		return
	}

	if !AITranslateEnabled() {
		renderJSON(w, http.StatusServiceUnavailable, apiError{
			Code:    http.StatusServiceUnavailable,
			Message: "AI translate not configured (set GRSAI_API_KEY)",
		})

		return
	}

	var req struct {
		Text        string `json:"text"`
		CountryName string `json:"country_name"`
		Lang        string `json:"lang"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: err.Error()})

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()

	out, err := AITranslateKeyword(ctx, req.Text, req.CountryName, req.Lang)
	if err != nil {
		renderJSON(w, http.StatusBadGateway, apiError{Code: http.StatusBadGateway, Message: err.Error()})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"translated": out,
		"lang":       req.Lang,
		"country":    req.CountryName,
	})
}

// apiGeocode 供前端「在哪里」预取坐标：支持中文海外地名（纽约/东京等），
// 走服务端 Nominatim，避免浏览器直连被限流或 CSP/CORS 拦住。
func (s *Server) apiGeocode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "missing q",
		})

		return
	}

	geoCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	point, err := Geocode(geoCtx, q)
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		})

		return
	}

	display := point.DisplayName
	if display == "" {
		display = q
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"lat":          point.Lat,
		"lon":          point.Lon,
		"country_code": point.CountryCode,
		"lang":         langForCountryCode(point.CountryCode),
		"display_name": display,
	})
}

// apiReverseGeocode 地图点选：经纬度 → 地点文案 + 国家（同步左侧表单）
func (s *Server) apiReverseGeocode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{
			Code:    http.StatusMethodNotAllowed,
			Message: "Method not allowed",
		})

		return
	}

	lat, err1 := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lat")), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lon")), 64)
	if err1 != nil || err2 != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "invalid lat/lon",
		})

		return
	}

	geoCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	point, err := ReverseGeocode(geoCtx, lat, lon)
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		})

		return
	}

	renderJSON(w, http.StatusOK, map[string]any{
		"lat":          point.Lat,
		"lon":          point.Lon,
		"country_code": point.CountryCode,
		"lang":         langForCountryCode(point.CountryCode),
		"display_name": point.DisplayName,
	})
}

// buildJobName 用关键词与地点拼任务显示名（取第一条关键词的原始词，去掉 " in 地点" 后缀）
func buildJobName(keywords []string, locations string) string {
	kw := ""
	if len(keywords) > 0 {
		kw = keywords[0]
		if locations != "" {
			kw = strings.TrimSuffix(kw, " in "+locations)
		}
		kw = strings.TrimSpace(kw)
	}

	locations = strings.TrimSpace(locations)
	switch {
	case kw != "" && locations != "":
		return locations + " · " + kw
	case kw != "":
		return kw
	default:
		return locations
	}
}

func (s *Server) apiScrape(w http.ResponseWriter, r *http.Request) {
	var req apiScrapeRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)

		return
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   req.Name,
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   req.JobData,
	}

	// convert to seconds
	newJob.Data.MaxTime *= time.Second
	// 一律深度全量：半径内不限数量，忽略客户端 fast/max_results/proxies
	ApplyFullVolumeDefaults(&newJob.Data, newJob.Data.Radius)

	err = newJob.Validate()
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)

		return
	}

	if err := s.attachOwner(r, &newJob); err != nil {
		renderJSON(w, http.StatusUnauthorized, apiError{
			Code:    http.StatusUnauthorized,
			Message: "需要有效邀请会话",
		})
		return
	}

	err = s.svc.Create(r.Context(), &newJob)
	if err != nil {
		ans := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, ans)

		return
	}

	ans := apiScrapeResponse{
		ID: newJob.ID,
	}

	renderJSON(w, http.StatusCreated, ans)
}

func (s *Server) apiGetJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.listJobsForRequest(r)
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)

		return
	}
	if jobs == nil {
		jobs = []Job{}
	}

	renderJSON(w, http.StatusOK, jobs)
}

func (s *Server) apiGetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)

		return
	}

	job, err := s.loadAccessibleJob(r, id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusNotFound,
			Message: http.StatusText(http.StatusNotFound),
		}

		renderJSON(w, http.StatusNotFound, apiError)

		return
	}

	renderJSON(w, http.StatusOK, job)
}

// apiGetPlaces returns the job's mappable places (parsed from its CSV output)
// as JSON. A job without CSV output yet yields an empty list, not an error.
// Query lite=1 (default for UI) returns a compact payload for fast first paint.
func (s *Server) apiGetPlaces(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)

		return
	}

	if _, err := s.loadAccessibleJob(r, id.String()); err != nil {
		renderJSON(w, http.StatusNotFound, apiError{
			Code:    http.StatusNotFound,
			Message: http.StatusText(http.StatusNotFound),
		})
		return
	}

	lite := r.URL.Query().Get("full") != "1"
	if lite {
		places, err := s.svc.GetPlacesLiteCached(r.Context(), id.String())
		if err != nil {
			if !errors.Is(err, ErrPlacesNotFound) {
				log.Printf("api get places lite %s: %v", id, err)
				renderJSON(w, http.StatusInternalServerError, apiError{
					Code:    http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
				})
				return
			}
			places = []PlaceLite{}
		}
		for i := range places {
			if places[i].Thumbnail != "" {
				tmp := Place{Thumbnail: places[i].Thumbnail, StreetViewURL: ""}
				rewritePlaceMedia(&tmp)
				places[i].Thumbnail = tmp.Thumbnail
			}
		}
		w.Header().Set("Cache-Control", "private, max-age=2")
		renderJSON(w, http.StatusOK, places)
		return
	}

	places, err := s.svc.GetPlacesCached(r.Context(), id.String())

	if err != nil {
		if !errors.Is(err, ErrPlacesNotFound) {
			log.Printf("api get places %s: %v", id, err)

			apiError := apiError{
				Code:    http.StatusInternalServerError,
				Message: http.StatusText(http.StatusInternalServerError),
			}

			renderJSON(w, http.StatusInternalServerError, apiError)

			return
		}

		places = []Place{}
	}

	for i := range places {
		rewritePlaceMedia(&places[i])
	}

	renderJSON(w, http.StatusOK, places)
}

func (s *Server) apiGetPlacesCount(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}
	if _, err := s.loadAccessibleJob(r, id.String()); err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: http.StatusNotFound, Message: "Not found"})
		return
	}
	n, err := s.svc.CountPlacesCached(r.Context(), id.String())
	if err != nil {
		if errors.Is(err, ErrPlacesNotFound) {
			renderJSON(w, http.StatusOK, map[string]int{"count": 0})
			return
		}
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: "internal error"})
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=2")
	renderJSON(w, http.StatusOK, map[string]int{"count": n})
}

func (s *Server) apiCancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}
	owner := s.requestOwner(r)
	if owner == "" && InviteRequired() {
		renderJSON(w, http.StatusUnauthorized, apiError{Code: http.StatusUnauthorized, Message: "需要邀请会话"})
		return
	}
	var err error
	if owner != "" {
		err = s.svc.CancelOwned(r.Context(), id.String(), owner)
	} else {
		job, gerr := s.svc.Get(r.Context(), id.String())
		if gerr != nil {
			err = gerr
		} else {
			s.svc.signalCancel(id.String())
			job.Status = StatusCanceled
			err = s.svc.Update(r.Context(), &job)
		}
	}
	if err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: err.Error()})
		return
	}
	renderJSON(w, http.StatusOK, map[string]string{"id": id.String(), "status": StatusCanceled})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}
	owner := s.requestOwner(r)
	if InviteRequired() && owner == "" {
		http.Error(w, "需要邀请会话", http.StatusUnauthorized)
		return
	}
	var err error
	if owner != "" {
		err = s.svc.CancelOwned(r.Context(), id.String(), owner)
	} else {
		job, gerr := s.svc.Get(r.Context(), id.String())
		if gerr != nil {
			err = gerr
		} else {
			s.svc.signalCancel(id.String())
			job.Status = StatusCanceled
			err = s.svc.Update(r.Context(), &job)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiPlaceIntel 返回/生成商家背调（公司架构 + 决策人联系方式）
func (s *Server) apiPlaceIntel(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}
	placeID := strings.TrimSpace(r.PathValue("place_id"))
	if placeID == "" {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "missing place_id"})
		return
	}

	job, err := s.loadAccessibleJob(r, id.String())
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: http.StatusNotFound, Message: "Job not found"})
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1" || r.Method == http.MethodPost
	if !refresh {
		if cached, ok := s.svc.loadIntel(id.String(), placeID); ok {
			rewriteIntelMedia(cached)
			renderJSON(w, http.StatusOK, cached)
			return
		}
	}

	places, err := s.svc.GetPlaces(r.Context(), id.String())
	if err != nil && !errors.Is(err, ErrPlacesNotFound) {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: err.Error()})
		return
	}
	var place Place
	found := false
	for _, p := range places {
		ensurePlaceKey(&p)
		if p.PlaceID == placeID || p.Cid == placeID || p.DataID == placeID || StablePlaceKey(p) == placeID {
			place = p
			found = true
			break
		}
	}
	if !found {
		renderJSON(w, http.StatusNotFound, apiError{Code: http.StatusNotFound, Message: "place not found in job results"})
		return
	}
	if place.PlaceID == "" {
		place.PlaceID = placeID
	}

	// 任务开启了并发背调：未就绪时异步生成并立即返回「背调中」，不阻塞请求线程
	if job.Data.EnableIntel && !refresh {
		s.svc.EnsurePlaceIntelAsync(id.String(), place)
		if cached, ok := s.svc.loadIntel(id.String(), place.PlaceID); ok {
			rewriteIntelMedia(cached)
			renderJSON(w, http.StatusOK, cached)
			return
		}
		renderJSON(w, http.StatusOK, PlaceIntel{
			PlaceID: place.PlaceID,
			Title:   place.Title,
			Website: place.Website,
			Status:  IntelRunning,
			Note:    "背调中",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	if refresh {
		_ = s.svc.deleteIntel(id.String(), place.PlaceID)
	}
	intel, err := s.svc.BuildPlaceIntel(ctx, id.String(), place)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: err.Error()})
		return
	}
	rewriteIntelMedia(intel)
	renderJSON(w, http.StatusOK, intel)
}

func (s *Server) apiJobIntelStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}
	job, err := s.loadAccessibleJob(r, id.String())
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: http.StatusNotFound, Message: "Job not found"})
		return
	}
	places, err := s.svc.GetPlaces(r.Context(), id.String())
	n := 0
	if err == nil {
		n = len(places)
	}
	st := s.svc.GetJobIntelStatus(id.String(), n)
	renderJSON(w, http.StatusOK, map[string]any{
		"status":       st,
		"enable_intel": job.Data.EnableIntel,
	})
}

func (s *Server) apiOSINTStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	st := ProbeOSINTTools()
	renderJSON(w, http.StatusOK, map[string]any{
		"tools":              st,
		"theharvester":       st.TheHarvester,
		"spiderfoot":         st.SpiderFoot,
		"holehe":             st.Holehe,
		"maigret":            st.Maigret,
		"blackbird":          st.Blackbird,
		"photon":             st.Photon,
		"amass":              st.Amass,
		"opencorporates_api": st.OpenCorporatesAPI,
		"hunter":             st.Hunter,
		"katana":             st.Katana,
		"github_commits":     st.GitHubCommits,
		"gleif":              st.GLEIF,
		"wikidata":           st.Wikidata,
		"ahu":                st.AHU,
		"ahu_proxy":          st.AHUProxyConfigured,
		"rdap":               st.RDAP,
		"crtsh":              st.CRTSH,
		"wayback":            st.Wayback,
		"wikipedia":          st.Wikipedia,
		"duckduckgo":         st.DuckDuckGo,
		"importyeti":         st.ImportYeti,
		"kirchner":           st.Kirchner,
		"crosslinked":        st.CrossLinked,
		"leadcontact":        st.LeadContact,
		"max_radius_km":      MaxRadiusKm(),
		"hint":               "bash tools/install_osint.sh；CrossLinked(Bing员工名)+Maigret(社媒画像)+公开源；AHU=印尼 ahu.go.id 董事登记（需 AHU_PROXY）；可选 HUNTER_API_KEY / LEADCONTACT_API_KEY",
	})
}

// viewJob renders the map modal fragment for a job, embedding the job's places
// directly so the client needs no separate data request.
func (s *Server) viewJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	job, jerr := s.loadAccessibleJob(r, id.String())
	if jerr != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// 轻量打开：默认不嵌入全量 places（前端 API 流式拉），大幅加快弹窗首屏
	lite := r.URL.Query().Get("lite") != "0"
	var places []Place
	if !lite {
		var err error
		places, err = s.svc.GetPlaces(r.Context(), id.String())
		if err != nil {
			if !errors.Is(err, ErrPlacesNotFound) {
				log.Printf("view job %s: %v", id, err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			places = []Place{}
		}
	} else {
		places = []Place{}
	}

	tmpl, ok := s.tmpl["static/templates/job_view.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	// 附带上任务 ID / 状态 / 是否背调：前端据此流式追加与门禁展开
	status := job.Status
	jobName := job.Name
	enableIntel := job.Data.EnableIntel

	// 必须 JSON 编码后再嵌入 <script>：直接 {{ .Places }} 会输出 Go 结构体文本，
	// 有结果时 JS 直接语法错误，导致弹窗右侧/左侧地图整段脚本不执行。
	placesJS, err := jsonJS(places)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	jobIDJS, _ := jsonJS(id.String())
	statusJS, _ := jsonJS(status)
	jobNameJS, _ := jsonJS(jobName)
	enableIntelJS, _ := jsonJS(enableIntel)
	liteJS, _ := jsonJS(lite)
	canExport := status == StatusOK
	if !canExport && status == StatusWorking {
		if _, csvErr := s.svc.GetCSV(r.Context(), id.String()); csvErr == nil {
			canExport = true
		}
	}

	viewData := map[string]any{
		"JobID":           id.String(),
		"JobIDJSON":       jobIDJS,
		"StatusJSON":      statusJS,
		"JobNameJSON":     jobNameJS,
		"PlacesJSON":      placesJS,
		"EnableIntelJSON": enableIntelJS,
		"LiteJSON":        liteJS,
		"CanExport":       canExport,
		"JobStatus":       status,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, viewData); err != nil {
		log.Printf("view job %s: render: %v", id, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	_, _ = buf.WriteTo(w)
}

func (s *Server) apiDeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)

		return
	}

	if _, err := s.loadAccessibleJob(r, id.String()); err != nil {
		renderJSON(w, http.StatusNotFound, apiError{
			Code:    http.StatusNotFound,
			Message: http.StatusText(http.StatusNotFound),
		})
		return
	}

	err := s.svc.Delete(r.Context(), id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func renderJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(data)
}

func formatDate(t time.Time) string {
	return t.Format("Jan 02, 2006 15:04:05")
}

// parseTargetRadiusMeters 解析目标半径：优先 radius_km（公里），否则 radius（米）。
// 结果钳制到 (0, MaxRadiusMeters]，默认 10km。
func parseTargetRadiusMeters(r *http.Request) (int, error) {
	if kmStr := strings.TrimSpace(r.Form.Get("radius_km")); kmStr != "" {
		km, err := strconv.ParseFloat(kmStr, 64)
		if err != nil || km <= 0 {
			return 0, fmt.Errorf("invalid radius_km")
		}
		if km > float64(MaxRadiusKm()) {
			return 0, fmt.Errorf("radius_km must be ≤ %d", MaxRadiusKm())
		}
		meters := int(km * 1000)
		if meters < 1000 {
			meters = 1000 // 至少 1km，避免过碎网格
		}
		return meters, nil
	}

	raw := strings.TrimSpace(r.Form.Get("radius"))
	if raw == "" {
		return 10000, nil // 默认 10km
	}
	meters, err := strconv.Atoi(raw)
	if err != nil || meters <= 0 {
		return 0, fmt.Errorf("invalid radius")
	}
	if meters > MaxRadiusMeters() {
		meters = MaxRadiusMeters()
	}
	return meters, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' cdn.tailwindcss.com cdnjs.cloudflare.com unpkg.com cdn.redoc.ly; "+
				"worker-src 'self' blob:; "+
				"style-src 'self' 'unsafe-inline' fonts.googleapis.com cdnjs.cloudflare.com unpkg.com; "+
				"img-src 'self' data: blob: cdn.redoc.ly cdnjs.cloudflare.com unpkg.com "+
				"*.tile.openstreetmap.org tile.openstreetmap.org "+
				"*.basemaps.cartocdn.com basemaps.cartocdn.com *.is.autonavi.com "+
				"*.googleusercontent.com streetviewpixels-pa.googleapis.com "+
				"images.contactout.com *.licdn.com; "+
				"font-src 'self' fonts.gstatic.com; "+
				"connect-src 'self' nominatim.openstreetmap.org")

		next.ServeHTTP(w, r)
	})
}
