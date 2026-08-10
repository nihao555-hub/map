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
		Name:   r.Form.Get("name"),
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

	keywords := strings.Split(keywordsStr[0], "\n")
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}

		// 如果有地点，把地点拼到关键词后面（加 in 前缀）
		if locationsStr != "" {
			k = k + " in " + locationsStr
		}

		newJob.Data.Keywords = append(newJob.Data.Keywords, k)
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

	newJob.Data.Email = r.Form.Get("email") == "on"

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

	// 结果列配置（快速模式可不选；深度/网格模式用户自选表头）
	newJob.Data.Columns = strings.TrimSpace(r.Form.Get("columns"))

	// 目标客户数量上限：0 或不填 = 不限
	if mr := strings.TrimSpace(r.Form.Get("maxresults")); mr != "" {
		if v, err := strconv.Atoi(mr); err == nil && v > 0 {
			newJob.Data.MaxResults = v
		}
	}

	// 地理锚定：普通模式（非网格）下，用户只填了地点没填有效经纬度时，
	// 先地理编码一次，把搜索锚定到目标地，并让 hl 与目标地语言匹配，
	// 避免 Google 按代理出口/浏览器环境本地化结果。
	// 地理编码失败只告警，回退为原来的未锚定搜索，不让任务失败
	if !newJob.Data.GridMode && locationsStr != "" && !hasGeoAnchor(newJob.Data.Lat, newJob.Data.Lon) {
		geoCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)

		point, geoErr := Geocode(geoCtx, locationsStr)

		cancel()

		if geoErr != nil {
			log.Printf("地理编码 %q 失败: %v，回退为未锚定搜索", locationsStr, geoErr)
		} else {
			newJob.Data.Lat = strconv.FormatFloat(point.Lat, 'f', 6, 64)
			newJob.Data.Lon = strconv.FormatFloat(point.Lon, 'f', 6, 64)

			if hl := langForCountryCode(point.CountryCode); hl != "" && hl != newJob.Data.Lang {
				log.Printf("地点 %q 国家代码 %s，hl 从 %s 调整为 %s",
					locationsStr, point.CountryCode, newJob.Data.Lang, hl)

				newJob.Data.Lang = hl
			}

			log.Printf("地点 %q 锚定到 %s,%s", locationsStr, newJob.Data.Lat, newJob.Data.Lon)
		}
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

	viewData := map[string]any{
		"JobID":  id.String(),
		"Status": status,
		"Places": places,
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
				"img-src 'self' data: cdn.redoc.ly cdnjs.cloudflare.com *.tile.openstreetmap.org *.is.autonavi.com; "+
				"font-src 'self' fonts.gstatic.com; "+
				"connect-src 'self'")

		next.ServeHTTP(w, r)
	})
}
