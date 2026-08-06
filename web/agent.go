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

// Agent standard pipeline (single user action → full run, no separate "understand/dispatch" UX):
//
//  1. IntentAgent     — NL → country / place / keywords / radius / intel flag
//  2. PlannerAgent    — split into atomic deep full-coverage scrape tasks
//  3. LocalizerAgent  — Maps-local keywords + geocode anchors
//  4. DispatcherAgent — create pending deep+grid+unlimited jobs
//  5. Scraper         — fair-admission browser scrape (queued if busy)
//  6. IntelAgent      — optional; starts after place results exist (on demand / post-scrape)
//
// Product policy: every job is deep + grid + MaxResults=0. Fast mode / quantity caps never used.

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
	Thinking    string   `json:"thinking,omitempty"` // user-facing reasoning (no tech fields)
	Source      string   `json:"source"`             // "ai" | "rules"
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

// AgentPipelineStep is one visible intermediate step in the standard agent run.
// Title/Summary are user-facing only — no internal role names or raw JSON.
type AgentPipelineStep struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"` // pending | active | complete | error
	Summary string `json:"summary"`
}

// AgentToolCall is a user-facing tool invocation for the AI Elements Tool UI.
type AgentToolCall struct {
	Name   string         `json:"name"`
	Title  string         `json:"title"`
	Status string         `json:"status"` // pending | running | complete | error
	Input  map[string]any `json:"input,omitempty"`
	Output string         `json:"output,omitempty"`
}

// AgentDispatchResult is the full-pipeline output (Intent→…→Dispatcher).
type AgentDispatchResult struct {
	Plan     AgentPlan           `json:"plan"`
	JobIDs   []string            `json:"job_ids"`
	Message  string              `json:"message"`
	Thinking string              `json:"thinking,omitempty"`
	Steps    []AgentPipelineStep `json:"steps"`
	Tools    []AgentToolCall     `json:"tools,omitempty"`
	Model    string              `json:"model"`
	Source   string              `json:"source"` // ai | rules
}

type agentIntentJSON struct {
	Thinking    string   `json:"thinking"`
	CountryCode string   `json:"country_code"`
	CountryName string   `json:"country_name"`
	Location    string   `json:"location"`
	Keywords    []string `json:"keywords"`
	RadiusKm    int      `json:"radius_km"`
	EnableIntel bool     `json:"enable_intel"`
	Notes       string   `json:"notes"`
	// Optional AI-proposed coverage anchors (商圈/区县) for multi-task split.
	SubLocations []string `json:"sub_locations"`
}

type agentPlanTaskJSON struct {
	Name     string   `json:"name"`
	Location string   `json:"location"`
	Keywords []string `json:"keywords"`
	RadiusKm int      `json:"radius_km"`
}

type agentPlanJSON struct {
	Thinking string              `json:"thinking"`
	Tasks    []agentPlanTaskJSON `json:"tasks"`
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

// cityCountryHint maps famous cities → country when the user omits the country name.
var cityCountryHint = map[string]struct{ Code, Name, Lang string }{
	"北京": {"cn", "China", "zh"}, "beijing": {"cn", "China", "zh"},
	"上海": {"cn", "China", "zh"}, "shanghai": {"cn", "China", "zh"},
	"广州": {"cn", "China", "zh"}, "guangzhou": {"cn", "China", "zh"},
	"深圳": {"cn", "China", "zh"}, "shenzhen": {"cn", "China", "zh"},
	"成都": {"cn", "China", "zh"}, "杭州": {"cn", "China", "zh"}, "hangzhou": {"cn", "China", "zh"},
	"重庆": {"cn", "China", "zh"}, "武汉": {"cn", "China", "zh"}, "西安": {"cn", "China", "zh"},
	"南京": {"cn", "China", "zh"}, "苏州": {"cn", "China", "zh"}, "天津": {"cn", "China", "zh"},
	"雅加达": {"id", "Indonesia", "id"}, "jakarta": {"id", "Indonesia", "id"},
	"泗水": {"id", "Indonesia", "id"}, "surabaya": {"id", "Indonesia", "id"},
	"曼谷": {"th", "Thailand", "th"}, "bangkok": {"th", "Thailand", "th"},
	"胡志明": {"vn", "Vietnam", "vi"}, "河内": {"vn", "Vietnam", "vi"}, "hanoi": {"vn", "Vietnam", "vi"},
	"吉隆坡": {"my", "Malaysia", "ms"}, "kuala lumpur": {"my", "Malaysia", "ms"},
	"马尼拉": {"ph", "Philippines", "tl"}, "manila": {"ph", "Philippines", "tl"},
	"新加坡": {"sg", "Singapore", "en"},
	"纽约":  {"us", "United States", "en"}, "new york": {"us", "United States", "en"},
	"洛杉矶": {"us", "United States", "en"}, "东京": {"jp", "Japan", "ja"}, "tokyo": {"jp", "Japan", "ja"},
	"首尔": {"kr", "South Korea", "ko"}, "迪拜": {"ae", "United Arab Emirates", "en"}, "dubai": {"ae", "United Arab Emirates", "en"},
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
	explicitRadius := radiusExplicitlyStated(goal)
	if in.RadiusKm <= 0 {
		in.RadiusKm = preferCoverageRadiusKm(in.Location, goal, 0, false)
	} else if !explicitRadius {
		// AI/rules may undershoot; bump known cities toward full coverage.
		in.RadiusKm = preferCoverageRadiusKm(in.Location, goal, in.RadiusKm, false)
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

func radiusExplicitlyStated(goal string) bool {
	return reRadiusKm.MatchString(goal) || reRadiusM.MatchString(goal)
}

var reCityWide = regexp.MustCompile(`整个|全市|全城|都会区|metropolitan|whole\s+city|city[- ]?wide|all\s+of\s+|覆盖`)

// preferCoverageRadiusKm chooses a radius that aims to finish the named place.
// Product: maximize recall inside the user's target location.
// Only auto-bumps when radius was missing/tiny (typical AI default ≤10) or city-wide wording;
// honors larger intentional radii and any km the user typed.
func preferCoverageRadiusKm(location, goal string, current int, explicit bool) int {
	if explicit && current > 0 {
		return current
	}
	loc := strings.TrimSpace(location)
	lowGoal := strings.ToLower(goal)
	cityWide := reCityWide.MatchString(goal)
	metro := isKnownMetro(loc) || isKnownMetroFromGoal(goal)
	if metro || cityWide {
		if cityWide || current <= 0 || current <= 10 {
			return 40
		}
		return current
	}
	// Unspecified radius + some place name → still prefer city-scale over 3–10km samples.
	if current <= 0 {
		if loc != "" || strings.Contains(lowGoal, "在") || strings.Contains(lowGoal, " in ") {
			return 25
		}
		return 10
	}
	if current > 0 && current <= 10 && loc != "" && !looksLikeSmallArea(loc, goal) {
		return 25
	}
	return current
}

func isKnownMetro(location string) bool {
	low := strings.ToLower(strings.TrimSpace(location))
	if low == "" {
		return false
	}
	for city := range cityCountryHint {
		if low == strings.ToLower(city) || strings.Contains(low, strings.ToLower(city)) {
			return true
		}
	}
	return false
}

func isKnownMetroFromGoal(goal string) bool {
	low := strings.ToLower(goal)
	for city := range cityCountryHint {
		if strings.Contains(goal, city) || strings.Contains(low, strings.ToLower(city)) {
			return true
		}
	}
	return false
}

func looksLikeSmallArea(location, goal string) bool {
	s := location + " " + goal
	return regexp.MustCompile(`(?i)mall|plaza|街|路|巷|小区|街区|商圈|market|station|机场|airport|码头|港区|园区`).MatchString(s)
}

// metroDistricts returns overlapping district anchors so a named metro can be
// fully scraped (one pin + 40km still misses some outskirts; districts help).
func metroDistricts(location string) []string {
	low := strings.ToLower(strings.TrimSpace(location))
	switch {
	case strings.Contains(low, "jakarta") || strings.Contains(location, "雅加达"):
		return []string{
			"Jakarta Pusat", "Jakarta Selatan", "Jakarta Barat", "Jakarta Utara", "Jakarta Timur",
		}
	case strings.Contains(low, "bangkok") || strings.Contains(location, "曼谷"):
		return []string{"Bangkok", "Nonthaburi", "Samut Prakan"}
	case strings.Contains(low, "surabaya") || strings.Contains(location, "泗水"):
		return []string{"Surabaya"}
	case strings.Contains(low, "manila") || strings.Contains(location, "马尼拉"):
		return []string{"Manila", "Makati", "Quezon City", "Pasig"}
	case strings.Contains(low, "kuala lumpur") || strings.Contains(location, "吉隆坡"):
		return []string{"Kuala Lumpur", "Petaling Jaya", "Shah Alam"}
	case strings.Contains(location, "北京") && !strings.Contains(location, "区"):
		return []string{"北京朝阳区", "北京海淀区", "北京东城区", "北京西城区", "北京丰台区", "北京通州区"}
	case strings.Contains(location, "上海") && !strings.Contains(location, "区"):
		return []string{"上海静安区", "上海黄浦区", "上海徐汇区", "上海浦东新区", "上海长宁区", "上海闵行区"}
	case strings.Contains(location, "广州") && !strings.Contains(location, "区"):
		return []string{"广州天河区", "广州越秀区", "广州海珠区", "广州白云区", "广州番禺区"}
	case strings.Contains(location, "深圳") && !strings.Contains(location, "区"):
		return []string{"深圳南山区", "深圳福田区", "深圳罗湖区", "深圳宝安区", "深圳龙岗区"}
	default:
		return districtHubs(location)
	}
}

// districtHubs expands a large urban district into commercial anchors.
func districtHubs(location string) []string {
	switch {
	case strings.Contains(location, "朝阳"):
		return []string{"北京朝阳国贸", "北京朝阳望京", "北京朝阳三里屯", "北京朝阳双井", "北京朝阳常营", "北京朝阳定福庄"}
	case strings.Contains(location, "海淀"):
		return []string{"北京海淀中关村", "北京海淀五道口", "北京海淀西二旗", "北京海淀万柳"}
	case strings.Contains(location, "浦东"):
		return []string{"上海陆家嘴", "上海张江", "上海金桥", "上海世纪公园"}
	case strings.Contains(location, "天河"):
		return []string{"广州天河城", "广州珠江新城", "广州岗顶"}
	case strings.Contains(location, "南山区") || (strings.Contains(location, "南山") && strings.Contains(location, "深圳")):
		return []string{"深圳南山科技园", "深圳南山后海", "深圳南山蛇口"}
	default:
		return nil
	}
}

// coverageAnchors picks scrape pins for a location (AI sub_locations > metro/district hubs).
func coverageAnchors(intent AgentIntent) ([]string, int) {
	radius := intent.RadiusKm
	if locs := parseSubLocationsFromNotes(intent.Notes); len(locs) > 1 {
		r := radius
		if r >= 20 {
			r = 12
		} else if r >= 12 {
			r = 8
		}
		if r < 5 {
			r = 5
		}
		return locs, r
	}
	if intent.Location == "" {
		return []string{""}, radius
	}
	if radius >= 15 {
		if districts := metroDistricts(intent.Location); len(districts) > 1 {
			r := 18
			if radius < 18 {
				r = radius
			}
			if strings.Contains(intent.Location, "区") || len(districts) >= 4 && radius <= 25 {
				// District hubs are denser — smaller circles.
				if r > 10 {
					r = 10
				}
			}
			return districts, r
		}
	}
	return []string{intent.Location}, radius
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
Goal: cover ALL matching businesses in the user's named place (max recall), not a small sample.
Extract structured search intent from the user's natural language goal.
Rules:
- thinking: 2–5 short sentences in the user's UI language explaining your understanding and why you may split work. NO technical field names, NO JSON keys, NO model/API jargon. Write for a business user.
- country_code: ISO 3166-1 alpha-2 lowercase when clear, else empty
- location: city/area to search (not the whole country name unless that is the place)
- keywords: 1-5 Google Maps category phrases. Split compound asks (e.g. cafes AND importers) into separate keywords.
- radius_km: integer 1-50. Prefer FULL coverage of the named place:
  * city / metro / "整个/全市/whole city" / city name only → 40
  * large urban district (区/县) → 15–25
  * neighborhood / mall / street / small area → 5–10
  * ONLY use a small radius when the user explicitly names a small area or gives a small km
  * if user gives an explicit km, honor it
- enable_intel: true if user asks for background check / decision makers / OSINT OR for lead-gen / 获客 goals (default true for 获客)
- sub_locations: when the place is a large city OR a big district that needs multiple scrape anchors, list 3–8 commercial hubs / sub-districts INSIDE that place (same language as Maps search). Examples:
  * 雅加达 → Jakarta Pusat, Jakarta Selatan, ...
  * 北京市朝阳区 → 朝阳国贸, 朝阳望京, 朝阳三里屯, 朝阳双井, 朝阳常营
  * single small street → empty array
Reply ONLY valid JSON object with keys: thinking, country_code, country_name, location, keywords, radius_km, enable_intel, notes, sub_locations`

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

	notes := j.Notes
	if len(j.SubLocations) > 0 {
		notes = strings.TrimSpace(notes + " | sub_locations=" + strings.Join(j.SubLocations, ";"))
	}
	return AgentIntent{
		Thinking:    strings.TrimSpace(j.Thinking),
		CountryCode: j.CountryCode,
		CountryName: j.CountryName,
		Location:    j.Location,
		Keywords:    j.Keywords,
		RadiusKm:    j.RadiusKm,
		EnableIntel: j.EnableIntel,
		Notes:       notes,
	}, nil
}

func parseSubLocationsFromNotes(notes string) []string {
	const marker = "sub_locations="
	idx := strings.Index(notes, marker)
	if idx < 0 {
		return nil
	}
	raw := strings.TrimSpace(notes[idx+len(marker):])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func understandIntentRules(goal, uiLang string) AgentIntent {
	intent := AgentIntent{
		RawGoal:  goal,
		UILang:   uiLang,
		RadiusKm: 0, // filled by preferCoverageRadiusKm unless explicit
	}

	low := strings.ToLower(goal)
	for alias, meta := range countryAlias {
		if strings.Contains(low, strings.ToLower(alias)) || strings.Contains(goal, alias) {
			intent.CountryCode = meta.Code
			intent.CountryName = meta.Name
			break
		}
	}

	// Infer country from well-known city names when not stated explicitly.
	if intent.CountryCode == "" {
		for city, meta := range cityCountryHint {
			if strings.Contains(goal, city) || strings.Contains(low, strings.ToLower(city)) {
				intent.CountryCode = meta.Code
				intent.CountryName = meta.Name
				break
			}
		}
	}

	explicit := false
	if m := reRadiusKm.FindStringSubmatch(goal); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			intent.RadiusKm = n
			explicit = true
		}
	} else if m := reRadiusM.FindStringSubmatch(goal); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1000 {
			intent.RadiusKm = n / 1000
			explicit = true
		}
	}
	if !explicit {
		intent.RadiusKm = preferCoverageRadiusKm(intent.Location, goal, intent.RadiusKm, false)
	}
	if intent.RadiusKm > MaxRadiusKm() {
		intent.RadiusKm = MaxRadiusKm()
	}

	intent.EnableIntel = reIntel.MatchString(goal)

	// Heuristic location: after 在/去/到, or 「找A的B」 place A, or in/near/around
	locPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:在|去|到|覆盖)\s*([^\s,，。；;]{2,24}?)(?:\s*(?:找|搜|抓|采集的|的|周围|周边|半径|,|，|。)|$)`),
		// 帮我找北京市朝阳区的火锅店 → location=北京市朝阳区
		regexp.MustCompile(`(?:找|搜|搜索|采集|挖掘)\s*([^\s,，。；;]{2,24}?)的([^\s,，。；;]{2,20})`),
		regexp.MustCompile(`(?i)(?:in|near|around|cover(?:ing)?)\s+([A-Za-z][A-Za-z0-9\s\-]{1,40}?)(?:\s+(?:find|search|for|within|,|\.|$))`),
	}
	var zhPlaceKeyword string
	for i, p := range locPatterns {
		if m := p.FindStringSubmatch(goal); len(m) >= 2 {
			loc := strings.TrimSpace(m[1])
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
				if i == 1 && len(m) == 3 {
					zhPlaceKeyword = strings.TrimSpace(m[2])
				}
				break
			}
		}
	}
	// If still empty, take known city mentioned in goal (prefer longest match).
	if intent.Location == "" {
		best := ""
		for city := range cityCountryHint {
			if strings.Contains(goal, city) || strings.Contains(low, strings.ToLower(city)) {
				if len([]rune(city)) > len([]rune(best)) {
					best = city
				}
			}
		}
		intent.Location = best
	}
	// Prefer richer Chinese admin area when present, e.g. 北京市朝阳区.
	if intent.Location != "" {
		if m := regexp.MustCompile(regexp.QuoteMeta(intent.Location) + `(?:市)?(?:\p{Han}{1,8}?(?:区|县|市))?`).FindString(goal); m != "" {
			intent.Location = m
		}
	}
	// Re-apply coverage preference now that location is known.
	if !explicit {
		intent.RadiusKm = preferCoverageRadiusKm(intent.Location, goal, intent.RadiusKm, false)
	}

	// Keywords: after 找/搜/采集 or leftover business words
	if zhPlaceKeyword != "" {
		intent.Keywords = append(intent.Keywords, zhPlaceKeyword)
	}
	kwPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:找|搜|搜索|采集|挖掘)\s*([^\s,，。；;0-9]{2,30})`),
		regexp.MustCompile(`(?i)(?:find|search|scrape)\s+(?:for\s+)?([A-Za-z][A-Za-z0-9\s\-]{1,40})`),
	}
	for _, p := range kwPatterns {
		if m := p.FindStringSubmatch(goal); len(m) == 2 {
			kw := strings.TrimSpace(m[1])
			kw = strings.TrimSuffix(kw, "的")
			// Strip leading place when 「找地点的品类」 already parsed.
			if intent.Location != "" && strings.HasPrefix(kw, intent.Location) {
				kw = strings.TrimPrefix(kw, intent.Location)
				kw = strings.TrimPrefix(kw, "的")
			}
			if kw != "" && kw != intent.Location {
				intent.Keywords = append(intent.Keywords, kw)
			}
		}
	}
	intent.Keywords = cleanKeywordList(intent.Keywords)
	intent.Keywords = splitCompoundKeywords(intent.Keywords)
	if len(intent.Keywords) == 0 {
		// Last resort: whole goal as keyword if short
		if len([]rune(goal)) <= 40 && !strings.Contains(goal, " ") {
			intent.Keywords = []string{goal}
		}
	}

	intent = normalizeIntent(intent, goal, uiLang)
	return intent
}

// splitCompoundKeywords turns "咖啡馆和进口商" / "cafe and importer" into separate tasks.
func splitCompoundKeywords(ks []string) []string {
	var out []string
	for _, k := range ks {
		parts := regexp.MustCompile(`(?:\s*(?:和|与|及|、|/|,|，|\band\b)\s*)`).Split(k, -1)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return cleanKeywordList(out)
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

	locations, districtRadius := coverageAnchors(intent)
	if len(locations) > 1 && intent.Thinking == "" {
		intent.Thinking = fmt.Sprintf(
			"目标区域较大，我会拆成 %d 个区域锚点分别深度抓取，尽量把匹配商家找全。",
			len(locations),
		)
		plan.Intent = intent
	} else {
		plan.Intent = intent
	}

	for _, loc := range locations {
		for _, kw := range intent.Keywords {
			name := kw
			if loc != "" {
				name = loc + " · " + kw
			} else if intent.CountryName != "" {
				name = intent.CountryName + " · " + kw
			}
			plan.Tasks = append(plan.Tasks, AgentTask{
				Name:        name,
				CountryCode: intent.CountryCode,
				CountryName: intent.CountryName,
				Location:    loc,
				Keywords:    []string{kw},
				RadiusKm:    districtRadius,
				EnableIntel: true, // agent path always prepares intel once places appear
				Role:        "scraper",
			})
		}
	}
	return plan
}

// PlanTasksAI asks the LLM to refine multi-task splits when AI is enabled.
// Falls back to PlanTasks on any failure.
func PlanTasksAI(ctx context.Context, intent AgentIntent) AgentPlan {
	base := PlanTasks(intent)
	if !AITranslateEnabled() || len(base.Tasks) == 0 {
		return base
	}
	key := grsaiAPIKey()
	if key == "" {
		return base
	}

	system := `You are PlannerAgent for a Maps lead scraper.
Given a structured intent, produce a multi-task scrape plan that maximizes coverage.
Rules:
- thinking: short user-facing Chinese/English (match UI) explaining the split. No JSON keys, no API jargon.
- Prefer 3–12 tasks for cities/districts; 1 task only for a tiny street/mall.
- Each task: one location anchor + one keyword + radius_km 5–18 for anchors, up to 40 for single city pin.
- Split multiple keywords into separate tasks.
- name: short human label like "望京 · 火锅店"
Reply ONLY JSON: {"thinking":"...","tasks":[{"name":"...","location":"...","keywords":["..."],"radius_km":8}]}`

	payload, _ := json.Marshal(map[string]any{
		"goal":         intent.RawGoal,
		"location":     intent.Location,
		"country_code": intent.CountryCode,
		"keywords":     intent.Keywords,
		"radius_km":    intent.RadiusKm,
		"ui_lang":      intent.UILang,
		"seed_tasks":   len(base.Tasks),
	})
	user := "Intent JSON:\n" + string(payload)

	body, err := json.Marshal(aiChatRequest{
		Model:  grsaiModel(),
		Stream: false,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return base
	}
	url := grsaiHost() + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return base
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("PlannerAgent AI failed, use heuristic plan: %v", err)
		return base
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("PlannerAgent AI status %d: %s", resp.StatusCode, truncate(string(raw), 160))
		return base
	}
	var parsed aiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return base
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	var pj agentPlanJSON
	if err := json.Unmarshal([]byte(content), &pj); err != nil || len(pj.Tasks) == 0 {
		return base
	}

	out := AgentPlan{Intent: intent, Roles: base.Roles}
	if t := strings.TrimSpace(pj.Thinking); t != "" {
		out.Intent.Thinking = t
		intent.Thinking = t
	}
	for _, t := range pj.Tasks {
		kw := cleanKeywordList(t.Keywords)
		if len(kw) == 0 {
			continue
		}
		loc := strings.TrimSpace(t.Location)
		if loc == "" {
			loc = intent.Location
		}
		r := t.RadiusKm
		if r <= 0 {
			r = 10
		}
		if r > MaxRadiusKm() {
			r = MaxRadiusKm()
		}
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = loc + " · " + kw[0]
		}
		out.Tasks = append(out.Tasks, AgentTask{
			Name:        name,
			CountryCode: intent.CountryCode,
			CountryName: intent.CountryName,
			Location:    loc,
			Keywords:    kw[:1],
			RadiusKm:    r,
			EnableIntel: true,
			Role:        "scraper",
		})
	}
	if len(out.Tasks) == 0 {
		return base
	}
	// Prefer richer AI splits, but never shrink a good heuristic metro plan to a single task.
	if len(out.Tasks) < len(base.Tasks) && len(base.Tasks) >= 3 && len(out.Tasks) == 1 {
		base.Intent.Thinking = out.Intent.Thinking
		return base
	}
	return out
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
				// Agent path: auto-start intel once places appear (EnsurePlaceIntelAsync).
				EnableIntel: true,
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
