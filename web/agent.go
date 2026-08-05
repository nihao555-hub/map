package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Agent roles for natural-language → full-volume deep scrape workflow:
//
//	IntentAgent     — understand user goal (country / place / what / radius / intel)
//	PlannerAgent    — split multi-target goals into atomic scrape tasks
//	LocalizerAgent  — translate keywords to Maps-searchable local terms
//	DispatcherAgent — create deep+grid+unlimited jobs (radius is the only volume knob)
//	IntelAgent      — optional post-scrape enrichment (flag only at create time)
//
// Product policy: every dispatched job is deep mode + grid full coverage + MaxResults=0.
// Fast mode and fixed quantity caps are never used.

// AgentIntent is the structured understanding of a user goal.
type AgentIntent struct {
	RawGoal     string   `json:"raw_goal"`
	CountryCode string   `json:"country_code"`
	CountryName string   `json:"country_name"`
	Location    string   `json:"location"`
	Keywords    []string `json:"keywords"`
	RadiusKm    int      `json:"radius_km"`
	EnableIntel bool     `json:"enable_intel"`
	UILang      string   `json:"ui_lang"`
	Notes       string   `json:"notes,omitempty"`
	Source      string   `json:"source"` // "ai" | "rules"
}

// AgentTask is one atomic scrape unit after planning.
type AgentTask struct {
	Name        string   `json:"name"`
	CountryCode string   `json:"country_code"`
	CountryName string   `json:"country_name"`
	Location    string   `json:"location"`
	Keywords    []string `json:"keywords"`
	RadiusKm    int      `json:"radius_km"`
	EnableIntel bool     `json:"enable_intel"`
	Role        string   `json:"role"` // always "scraper" for Maps jobs
}

// AgentPlan is PlannerAgent output.
type AgentPlan struct {
	Intent AgentIntent `json:"intent"`
	Tasks  []AgentTask `json:"tasks"`
	Roles  []string    `json:"roles"`
}

// AgentDispatchResult is DispatcherAgent output.
type AgentDispatchResult struct {
	Plan    AgentPlan `json:"plan"`
	JobIDs  []string  `json:"job_ids"`
	Message string    `json:"message"`
}

type agentIntentJSON struct {
	CountryCode string   `json:"country_code"`
	CountryName string   `json:"country_name"`
	Location    string   `json:"location"`
	Keywords    []string `json:"keywords"`
	RadiusKm    int      `json:"radius_km"`
	EnableIntel bool     `json:"enable_intel"`
	Notes       string   `json:"notes"`
}

var (
	reRadiusKm = regexp.MustCompile(`(?i)(\d+)\s*(?:km|公里|千米|kilometer|kilometre)`)
	reRadiusM  = regexp.MustCompile(`(?i)(\d+)\s*(?:m|米)(?:\b|[^a-z]|$)`)
	reIntel    = regexp.MustCompile(`(?i)背调|intel|osint|决策人|老板|联系人`)
)

// countryAlias maps common Chinese/English names → ISO + display + Maps hl.
var countryAlias = map[string]struct{ Code, Name, Lang string }{
	"印尼": {"id", "Indonesia", "id"}, "印度尼西亚": {"id", "Indonesia", "id"}, "indonesia": {"id", "Indonesia", "id"},
	"泰国": {"th", "Thailand", "th"}, "thailand": {"th", "Thailand", "th"},
	"越南": {"vn", "Vietnam", "vi"}, "vietnam": {"vn", "Vietnam", "vi"},
	"马来": {"my", "Malaysia", "ms"}, "马来西亚": {"my", "Malaysia", "ms"}, "malaysia": {"my", "Malaysia", "ms"},
	"新加坡": {"sg", "Singapore", "en"}, "singapore": {"sg", "Singapore", "en"},
	"菲律宾": {"ph", "Philippines", "tl"}, "philippines": {"ph", "Philippines", "tl"},
	"美国": {"us", "United States", "en"}, "usa": {"us", "United States", "en"}, "united states": {"us", "United States", "en"},
	"日本": {"jp", "Japan", "ja"}, "japan": {"jp", "Japan", "ja"},
	"韩国": {"kr", "South Korea", "ko"}, "korea": {"kr", "South Korea", "ko"},
	"中国": {"cn", "China", "zh"}, "china": {"cn", "China", "zh"},
	"澳洲": {"au", "Australia", "en"}, "澳大利亚": {"au", "Australia", "en"}, "australia": {"au", "Australia", "en"},
	"英国": {"gb", "United Kingdom", "en"}, "uk": {"gb", "United Kingdom", "en"},
	"德国": {"de", "Germany", "de"}, "germany": {"de", "Germany", "de"},
	"法国": {"fr", "France", "fr"}, "france": {"fr", "France", "fr"},
	"巴西": {"br", "Brazil", "pt"}, "brazil": {"br", "Brazil", "pt"},
	"墨西哥": {"mx", "Mexico", "es"}, "mexico": {"mx", "Mexico", "es"},
	"印度": {"in", "India", "en"}, "india": {"in", "India", "en"},
	"阿联酋": {"ae", "United Arab Emirates", "en"}, "dubai": {"ae", "United Arab Emirates", "en"},
	"柬埔寨": {"kh", "Cambodia", "km"}, "laos": {"la", "Laos", "lo"}, "老挝": {"la", "Laos", "lo"},
	"缅甸": {"mm", "Myanmar", "my"}, "myanmar": {"mm", "Myanmar", "my"},
}

// UnderstandIntent is IntentAgent: NL goal → structured intent.
func UnderstandIntent(ctx context.Context, goal, uiLang string) (AgentIntent, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return AgentIntent{}, fmt.Errorf("empty goal")
	}
	uiLang = normalizeUILang(uiLang)

	if AITranslateEnabled() {
		if intent, err := understandIntentAI(ctx, goal, uiLang); err == nil {
			intent = normalizeIntent(intent, goal, uiLang)
			intent.Source = "ai"
			return intent, nil
		} else {
			log.Printf("IntentAgent AI failed, fallback rules: %v", err)
		}
	}

	intent := understandIntentRules(goal, uiLang)
	intent.Source = "rules"
	return intent, nil
}

func normalizeIntent(in AgentIntent, goal, uiLang string) AgentIntent {
	in.RawGoal = goal
	in.UILang = uiLang
	if in.RadiusKm <= 0 {
		in.RadiusKm = 10
	}
	if in.RadiusKm > MaxRadiusKm() {
		in.RadiusKm = MaxRadiusKm()
	}
	in.Keywords = cleanKeywordList(in.Keywords)
	if in.CountryCode != "" {
		in.CountryCode = strings.ToLower(strings.TrimSpace(in.CountryCode))
		if in.CountryName == "" {
			for _, v := range countryAlias {
				if v.Code == in.CountryCode {
					in.CountryName = v.Name
					break
				}
			}
		}
	}
	return in
}

func cleanKeywordList(ks []string) []string {
	out := make([]string, 0, len(ks))
	seen := map[string]struct{}{}
	for _, k := range ks {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		low := strings.ToLower(k)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		out = append(out, k)
	}
	return out
}

func understandIntentAI(ctx context.Context, goal, uiLang string) (AgentIntent, error) {
	key := grsaiAPIKey()
	if key == "" {
		return AgentIntent{}, fmt.Errorf("no AI key")
	}

	system := `You are IntentAgent for a Google Maps lead scraper.
Extract structured search intent from the user's natural language goal.
Rules:
- country_code: ISO 3166-1 alpha-2 lowercase when clear, else empty
- location: city/area to search (not the whole country name unless that is the place)
- keywords: 1-5 Google Maps category phrases in the user's language (will be localized later)
- radius_km: integer 1-50; default 10 if unspecified; never invent huge radii
- enable_intel: true only if user asks for background check / decision makers / OSINT
Reply ONLY valid JSON object with keys: country_code, country_name, location, keywords, radius_km, enable_intel, notes`

	user := fmt.Sprintf("UI language: %s\nUser goal:\n%s", uiLang, goal)

	body, err := json.Marshal(aiChatRequest{
		Model:  grsaiModel(),
		Stream: false,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return AgentIntent{}, err
	}

	url := grsaiHost() + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return AgentIntent{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return AgentIntent{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AgentIntent{}, fmt.Errorf("intent AI status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var parsed aiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return AgentIntent{}, err
	}
	if len(parsed.Choices) == 0 {
		return AgentIntent{}, fmt.Errorf("empty AI choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var j agentIntentJSON
	if err := json.Unmarshal([]byte(content), &j); err != nil {
		return AgentIntent{}, fmt.Errorf("intent JSON: %w", err)
	}

	return AgentIntent{
		CountryCode: j.CountryCode,
		CountryName: j.CountryName,
		Location:    j.Location,
		Keywords:    j.Keywords,
		RadiusKm:    j.RadiusKm,
		EnableIntel: j.EnableIntel,
		Notes:       j.Notes,
	}, nil
}

func understandIntentRules(goal, uiLang string) AgentIntent {
	intent := AgentIntent{
		RawGoal:  goal,
		UILang:   uiLang,
		RadiusKm: 10,
	}

	low := strings.ToLower(goal)
	for alias, meta := range countryAlias {
		if strings.Contains(low, strings.ToLower(alias)) || strings.Contains(goal, alias) {
			intent.CountryCode = meta.Code
			intent.CountryName = meta.Name
			break
		}
	}

	if m := reRadiusKm.FindStringSubmatch(goal); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			intent.RadiusKm = n
		}
	} else if m := reRadiusM.FindStringSubmatch(goal); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1000 {
			intent.RadiusKm = n / 1000
		}
	}
	if intent.RadiusKm > MaxRadiusKm() {
		intent.RadiusKm = MaxRadiusKm()
	}

	intent.EnableIntel = reIntel.MatchString(goal)

	// Heuristic location: after 在/去/到 or in/near/around
	locPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:在|去|到)\s*([^\s,，。；;]{2,20}?)(?:\s*(?:找|搜|抓|采集|的|周围|周边|半径|,|，|。)|$)`),
		regexp.MustCompile(`(?i)(?:in|near|around)\s+([A-Za-z][A-Za-z0-9\s\-]{1,40}?)(?:\s+(?:find|search|for|within|,|\.|$))`),
	}
	for _, p := range locPatterns {
		if m := p.FindStringSubmatch(goal); len(m) == 2 {
			loc := strings.TrimSpace(m[1])
			// Don't treat country name as city
			if intent.CountryName != "" && (strings.EqualFold(loc, intent.CountryName) || loc == intent.CountryCode) {
				continue
			}
			skip := false
			for alias := range countryAlias {
				if loc == alias || strings.EqualFold(loc, alias) {
					skip = true
					break
				}
			}
			if !skip && loc != "" {
				intent.Location = loc
				break
			}
		}
	}

	// Keywords: after 找/搜/采集 or leftover business words
	kwPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:找|搜|搜索|采集|挖掘)\s*([^\s,，。；;0-9]{2,30})`),
		regexp.MustCompile(`(?i)(?:find|search|scrape)\s+(?:for\s+)?([A-Za-z][A-Za-z0-9\s\-]{1,40})`),
	}
	for _, p := range kwPatterns {
		if m := p.FindStringSubmatch(goal); len(m) == 2 {
			kw := strings.TrimSpace(m[1])
			kw = strings.TrimSuffix(kw, "的")
			if kw != "" && kw != intent.Location {
				intent.Keywords = append(intent.Keywords, kw)
			}
		}
	}
	intent.Keywords = cleanKeywordList(intent.Keywords)
	if len(intent.Keywords) == 0 {
		// Last resort: whole goal as keyword if short
		if len([]rune(goal)) <= 40 && !strings.Contains(goal, " ") {
			intent.Keywords = []string{goal}
		}
	}

	intent = normalizeIntent(intent, goal, uiLang)
	return intent
}

// PlanTasks is PlannerAgent: one intent → one or more deep full-scan tasks.
func PlanTasks(intent AgentIntent) AgentPlan {
	intent = normalizeIntent(intent, intent.RawGoal, intent.UILang)
	roles := []string{"IntentAgent", "PlannerAgent", "LocalizerAgent", "DispatcherAgent"}
	if intent.EnableIntel {
		roles = append(roles, "IntelAgent")
	}

	plan := AgentPlan{Intent: intent, Roles: roles}
	if len(intent.Keywords) == 0 {
		return plan
	}

	// Split multi-keyword into parallel tasks only when clearly different categories;
	// same location shared — each keyword gets its own full-radius deep grid job.
	for _, kw := range intent.Keywords {
		name := kw
		if intent.Location != "" {
			name = intent.Location + " · " + kw
		} else if intent.CountryName != "" {
			name = intent.CountryName + " · " + kw
		}
		plan.Tasks = append(plan.Tasks, AgentTask{
			Name:        name,
			CountryCode: intent.CountryCode,
			CountryName: intent.CountryName,
			Location:    intent.Location,
			Keywords:    []string{kw},
			RadiusKm:    intent.RadiusKm,
			EnableIntel: intent.EnableIntel,
			Role:        "scraper",
		})
	}
	return plan
}

// ApplyFullVolumeDefaults forces product policy onto a job: deep + grid + unlimited.
func ApplyFullVolumeDefaults(d *JobData, radiusMeters int) {
	d.FastMode = false
	d.GridMode = true
	d.MaxResults = 0
	d.Email = true
	d.Proxies = nil // server-side GMS_PROXIES only
	if radiusMeters > 0 {
		d.Radius = radiusMeters
	}
	if d.Radius <= 0 {
		d.Radius = 10_000
	}
	if d.Radius > MaxRadiusMeters() {
		d.Radius = MaxRadiusMeters()
	}
	if d.Depth < 20 {
		d.Depth = 50
	}
	if d.MaxTime < 60*time.Minute {
		d.MaxTime = 120 * time.Minute
	}
	if d.GridCellKm <= 0 {
		// Coarsen cells for larger radii so deep browser stays runnable
		km := float64(d.Radius) / 1000
		switch {
		case km >= 30:
			d.GridCellKm = 3.0
		case km >= 15:
			d.GridCellKm = 2.5
		default:
			d.GridCellKm = 2.0
		}
	}
	if d.Zoom <= 0 {
		d.Zoom = 15
	}
}

// DispatchPlan is DispatcherAgent: create pending jobs from a plan.
func (s *Server) DispatchPlan(ctx context.Context, owner string, plan AgentPlan) (AgentDispatchResult, error) {
	out := AgentDispatchResult{Plan: plan}
	if len(plan.Tasks) == 0 {
		return out, fmt.Errorf("PlannerAgent: no tasks (need keywords + location/country)")
	}

	for _, task := range plan.Tasks {
		if len(task.Keywords) == 0 {
			continue
		}
		if strings.TrimSpace(task.Location) == "" && strings.TrimSpace(task.CountryCode) == "" {
			return out, fmt.Errorf("task %q missing location/country", task.Name)
		}

		job := Job{
			ID:     uuid.New().String(),
			Name:   task.Name,
			Date:   time.Now().UTC(),
			Status: StatusPending,
			Owner:  owner,
			Data: JobData{
				RawKeywords: append([]string(nil), task.Keywords...),
				Locations:   task.Location,
				CountryCode: task.CountryCode,
				CountryName: task.CountryName,
				UILang:      normalizeUILang(plan.Intent.UILang),
				EnableIntel: task.EnableIntel,
				Lang:        "en",
			},
		}
		if hl := langForCountryCode(task.CountryCode); hl != "" {
			job.Data.Lang = hl
		}
		ApplyFullVolumeDefaults(&job.Data, task.RadiusKm*1000)

		// LocalizerAgent: translate keywords for Maps
		searchLoc := task.Location
		locCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		localized, _, _ := localizeSearchQuery(locCtx, task.Keywords, searchLoc, job.Data.Lang, localizeOpts{
			CountryName: task.CountryName,
			UseAI:       AITranslateEnabled(),
		})
		cancel()
		if len(localized) == 0 {
			localized = append([]string(nil), task.Keywords...)
		}
		if job.Data.Lang != "zh" {
			clean := make([]string, 0, len(localized))
			for _, kw := range localized {
				if !containsChinese(kw) {
					clean = append(clean, kw)
				}
			}
			if len(clean) > 0 {
				localized = clean
			}
		}
		job.Data.Keywords = localized

		// Geocode location when possible
		if task.Location != "" {
			geoCtx, gcancel := context.WithTimeout(ctx, 12*time.Second)
			point, geoErr := ResolveLocationAnchor(geoCtx, task.Location, task.CountryCode)
			gcancel()
			if geoErr == nil {
				job.Data.Lat = strconv.FormatFloat(point.Lat, 'f', 6, 64)
				job.Data.Lon = strconv.FormatFloat(point.Lon, 'f', 6, 64)
				if job.Data.CountryCode == "" && point.CountryCode != "" {
					job.Data.CountryCode = strings.ToLower(point.CountryCode)
					if hl := langForCountryCode(job.Data.CountryCode); hl != "" {
						job.Data.Lang = hl
					}
				}
			} else {
				log.Printf("DispatcherAgent geocode %q: %v", task.Location, geoErr)
			}
		}

		if err := job.Validate(); err != nil {
			return out, fmt.Errorf("job validate: %w", err)
		}
		if err := s.svc.Create(ctx, &job); err != nil {
			return out, err
		}
		out.JobIDs = append(out.JobIDs, job.ID)
		log.Printf("DispatcherAgent created job %s name=%q deep+grid unlimited radius=%dm",
			job.ID, job.Name, job.Data.Radius)
	}

	out.Message = fmt.Sprintf("已创建 %d 个深度全量任务（半径内不限数量）", len(out.JobIDs))
	return out, nil
}
