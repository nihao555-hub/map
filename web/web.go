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
	"path/filepath"
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
	tmpl map[string]*template.Template
	srv  *http.Server
	svc  *Service
}

func New(svc *Service, addr string) (*Server, error) {
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

	staticFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(staticFS))
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
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

	mux.HandleFunc("/api/v1/geocode", ans.apiGeocode)
	mux.HandleFunc("/api/v1/reverse-geocode", ans.apiReverseGeocode)
	mux.HandleFunc("/api/v1/ai-translate", ans.apiAITranslate)
	mux.HandleFunc("/api/v1/ai-status", ans.apiAIStatus)

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

	handler := securityHeaders(mux)
	ans.srv.Handler = handler

	tmplsKeys := []string{
		"static/templates/index.html",
		"static/templates/job_rows.html",
		"static/templates/job_row.html",
		"static/templates/job_view.html",
		"static/templates/redoc.html",
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

	if r.Form.Get("fastmode") == "on" {
		newJob.Data.FastMode = true
	}

	newJob.Data.Radius, err = strconv.Atoi(r.Form.Get("radius"))
	if err != nil {
		http.Error(w, "invalid radius", http.StatusUnprocessableEntity)

		return
	}

	newJob.Data.Lat = r.Form.Get("latitude")
	newJob.Data.Lon = r.Form.Get("longitude")

	newJob.Data.Depth, err = strconv.Atoi(r.Form.Get("depth"))
	if err != nil {
		http.Error(w, "invalid depth", http.StatusUnprocessableEntity)

		return
	}

	// 邮箱为获客刚需：快速/深度/网格一律开启，忽略前端关闭
	newJob.Data.Email = true

	// 网格全量模式
	if r.Form.Get("gridmode") == "on" {
		newJob.Data.GridMode = true
		// 网格边长（公里），默认 1.5
		cellKm := 1.5
		if cellStr := r.Form.Get("gridcell"); cellStr != "" {
			if v, err := strconv.ParseFloat(cellStr, 64); err == nil && v > 0 {
				cellKm = v
			}
		}
		newJob.Data.GridCellKm = cellKm
		// 保存原始地点名（用于地理编码生成 bbox）
		newJob.Data.Locations = locationsStr
		// 如果手动传了 bbox 就用手动的
		if bbox := r.Form.Get("gridbbox"); bbox != "" {
			newJob.Data.GridBBox = bbox
		}
		// 快速模式已原生支持网格（纯 HTTP 搜索接口按格取数），不再强制关闭
	}

	// 快速模式单点搜索结果很少：自动开粗网格扩量（仍走纯 HTTP）。
	// 格子边长/半径加以限制，避免上百格拖慢「抓满即停」的收尾。
	if newJob.Data.FastMode && !newJob.Data.GridMode {
		newJob.Data.GridMode = true
		newJob.Data.GridCellKm = 2.5
		if newJob.Data.Radius <= 0 || newJob.Data.Radius > 10000 {
			newJob.Data.Radius = 10000 // ±5km → 大约十几格
		}
		if newJob.Data.Locations == "" {
			newJob.Data.Locations = locationsStr
		}
		log.Printf("快速模式自动启用粗网格扩量 cell=%.1fkm radius=%dm", newJob.Data.GridCellKm, newJob.Data.Radius)
	}

	// 结果列配置（快速模式可不选；深度/网格模式用户自选表头）
	newJob.Data.Columns = strings.TrimSpace(r.Form.Get("columns"))

	// 目标客户数量上限：0 或不填 = 不限
	if mr := strings.TrimSpace(r.Form.Get("maxresults")); mr != "" {
		if v, err := strconv.Atoi(mr); err == nil && v > 0 {
			newJob.Data.MaxResults = v
		}
	}

	// 深度模式目标量大（≥100 或不限）时自动开粗网格，否则单点+偏移仍远达不到「全量」。
	// 必须在解析 maxresults 之后；用户点了「全域全量」时已是 GridMode，这里只补「深度 + 高上限」。
	if !newJob.Data.FastMode && !newJob.Data.GridMode &&
		(newJob.Data.MaxResults == 0 || newJob.Data.MaxResults >= 100) {
		newJob.Data.GridMode = true
		if newJob.Data.GridCellKm <= 0 {
			newJob.Data.GridCellKm = 2.0 // 深度走浏览器，格子稍粗以免格数爆炸
		}
		if newJob.Data.Radius <= 0 || newJob.Data.Radius > 15000 {
			newJob.Data.Radius = 15000 // ±7.5km
		}
		if newJob.Data.Locations == "" {
			newJob.Data.Locations = locationsStr
		}
		log.Printf("深度模式目标量大(max=%d)，自动启用粗网格全量 cell=%.1fkm radius=%dm",
			newJob.Data.MaxResults, newJob.Data.GridCellKm, newJob.Data.Radius)
	}

	// 用户显式选择的目标国家（优先于地理编码推断）
	countryCode := strings.ToLower(strings.TrimSpace(r.Form.Get("country_code")))
	countryName := strings.TrimSpace(r.Form.Get("country_name"))
	useAI := r.Form.Get("ai_translate") == "on" || r.Form.Get("ai_translate") == "true"
	newJob.Data.CountryCode = countryCode
	newJob.Data.CountryName = countryName
	newJob.Data.RawKeywords = append([]string(nil), rawKeywords...)
	if countryCode != "" {
		if hl := langForCountryCode(countryCode); hl != "" {
			newJob.Data.Lang = hl
		}
	}

	// 勾了 AI、关键词含中文，但服务端未配密钥：直接拒绝，避免中文进 Maps 只出 1～2 家
	needAI := useAI && newJob.Data.Lang != "zh"
	hasChineseKW := false
	for _, k := range rawKeywords {
		if containsChinese(k) {
			hasChineseKW = true
			break
		}
	}
	if needAI && hasChineseKW && !AITranslateEnabled() {
		http.Error(w, "已勾选 AI 翻译，但服务端未配置 GRSAI_API_KEY，无法把中文品类译成可搜词。请在 .env 配置密钥后重启，或改用英文关键词", http.StatusServiceUnavailable)

		return
	}

	// 地理锚定 + 海外中文查询本地化：
	// 1) 锚定经纬度 / 按国家校正 hl
	// 2) 把「咖啡 in 纽约」译成「coffee in New York」再交给 Google Maps
	searchLocation := locationsStr
	if locationsStr != "" {
		geoCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)

		point, geoErr := GeocodeInCountry(geoCtx, locationsStr, "en", countryCode)

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
	}

	// 任务名始终由服务端用当前关键词+地点生成，避免前端隐藏域残留导致「名实不符」
	newJob.Name = buildJobName(rawKeywords, locationsStr)

	// 关键词本地化：中文品类 → 目标国语言；地点用英文/当地名（词典 → AI → 机翻）
	{
		locTimeout := 12 * time.Second
		if useAI && AITranslateEnabled() {
			locTimeout = 55 * time.Second
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

	// 快速模式必须有真实地理锚点，否则会落到 (0,0) 而不是用户选的国家/城市
	if newJob.Data.FastMode && !hasGeoAnchor(newJob.Data.Lat, newJob.Data.Lon) {
		http.Error(w, "快速模式需要有效地点：请选择国家并在地图上选点，或等待地点解析完成后再提交", http.StatusUnprocessableEntity)

		return
	}

	// 提交前校验代理：格式非法、缺用户名密码认证的立即拒绝，
	// 不要等任务跑到启动 auth proxy 时才失败
	proxies, err := validateProxyLines(r.Form.Get("proxies"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	newJob.Data.Proxies = proxies

	err = newJob.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)

		return
	}

	log.Printf("任务意图 name=%q raw=%v search=%v country=%s(%s) lang=%s loc=%q geo=%s,%s fast=%v grid=%v",
		newJob.Name, newJob.Data.RawKeywords, newJob.Data.Keywords,
		newJob.Data.CountryName, newJob.Data.CountryCode, newJob.Data.Lang,
		newJob.Data.Locations, newJob.Data.Lat, newJob.Data.Lon,
		newJob.Data.FastMode, newJob.Data.GridMode)

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

	jobs, err := s.svc.All(context.Background())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	_ = tmpl.Execute(w, jobs)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ctx := r.Context()

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)

		return
	}

	filePath, err := s.svc.GetCSV(ctx, id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	fileName := filepath.Base(filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("Content-Type", "text/csv")

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

	err = newJob.Validate()
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)

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
	jobs, err := s.svc.All(r.Context())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)

		return
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

	job, err := s.svc.Get(r.Context(), id.String())
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

	places, err := s.svc.GetPlaces(r.Context(), id.String())

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

	renderJSON(w, http.StatusOK, places)
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

	places, err := s.svc.GetPlaces(r.Context(), id.String())

	if err != nil {
		if !errors.Is(err, ErrPlacesNotFound) {
			log.Printf("view job %s: %v", id, err)
			http.Error(w, "internal server error", http.StatusInternalServerError)

			return
		}

		// No CSV yet: render the modal with an empty state rather than an error.
		places = []Place{}
	}

	tmpl, ok := s.tmpl["static/templates/job_view.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	// 附带上任务 ID 与状态：前端据此在任务运行中流式追加结果行
	status := ""
	if job, jerr := s.svc.Get(r.Context(), id.String()); jerr == nil {
		status = job.Status
	}

	// 必须 JSON 编码后再嵌入 <script>：直接 {{ .Places }} 会输出 Go 结构体文本，
	// 有结果时 JS 直接语法错误，导致弹窗右侧/左侧地图整段脚本不执行。
	placesJS, err := jsonJS(places)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	jobIDJS, _ := jsonJS(id.String())
	statusJS, _ := jsonJS(status)

	viewData := map[string]any{
		"JobIDJSON":  jobIDJS,
		"StatusJSON": statusJS,
		"PlacesJSON": placesJS,
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
				"*.basemaps.cartocdn.com basemaps.cartocdn.com *.is.autonavi.com; "+
				"font-src 'self' fonts.gstatic.com; "+
				"connect-src 'self' nominatim.openstreetmap.org")

		next.ServeHTTP(w, r)
	})
}
