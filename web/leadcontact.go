package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// LeadContact（https://leadcontact.ai）— 领英定人 + 邮箱/电话富化。
//
// 计费（官方文档）：
//   个人资料查询 5 credits / 次
//   邮箱查询     10 credits / 次
//   电话查询     30 credits / 次
// 高级员工搜索会返回名单（含头像 URL / LinkedIn / 经历），再按需查邮箱电话。
//
// 环境变量：LEADCONTACT_API_KEY 或 LEADCONTACT_TOKEN

const leadContactBase = "https://api.leadcontact.ai"

func leadContactToken() string {
	for _, k := range []string{"LEADCONTACT_API_KEY", "LEADCONTACT_TOKEN", "LEAD_CONTACT_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// LeadContactEnabled 是否启用（密钥 + 显式开关，避免误烧 credits）。
// 设 LEADCONTACT_ENABLE=1 且配置 LEADCONTACT_API_KEY 才调用。
func LeadContactEnabled() bool {
	if leadContactToken() == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LEADCONTACT_ENABLE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type leadContactEmployee struct {
	FullName          string   `json:"fullName"`
	Title             string   `json:"title"`
	CompanyName       string   `json:"companyName"`
	ProfilePictureURL string   `json:"profilePictureUrl"`
	Summary           string   `json:"summary"`
	Country           string   `json:"country"`
	Location          string   `json:"location"`
	LinkedInURL       string   `json:"linkedinUrl"`
	PublicID          string   `json:"publicId"`
	Skills            []string `json:"skills"`
}

type leadContactSearchResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Employees          []leadContactEmployee `json:"employees"`
		TotalEmployeeCount int                   `json:"totalEmployeeCount"`
		NextPageToken      string                `json:"nextPageToken"`
	} `json:"data"`
}

type leadContactProfileResp struct {
	Code int                 `json:"code"`
	Msg  string              `json:"msg"`
	Data leadContactEmployee `json:"data"`
}

type leadContactContactResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Sources []struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Phone string `json:"phone"`
			Valid bool   `json:"valid"`
		} `json:"sources"`
	} `json:"data"`
}

func leadContactDo(ctx context.Context, method, path string, body any) ([]byte, error) {
	token := leadContactToken()
	if token == "" {
		return nil, fmt.Errorf("LEADCONTACT_API_KEY not set")
	}

	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, leadContactBase+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("leadcontact auth: %s", truncateRunes(string(raw), 120))
	}
	return raw, nil
}

// lookupLeadContactPeople 按公司名在印尼搜采购/老板决策人（高级搜索）。
func lookupLeadContactPeople(ctx context.Context, company, country string) ([]DecisionMaker, error) {
	company = strings.TrimSpace(company)
	if company == "" || !LeadContactEnabled() {
		return nil, fmt.Errorf("skip")
	}

	loc := []string{}
	if c := strings.TrimSpace(country); c != "" {
		loc = []string{c}
	} else {
		loc = []string{"Indonesia"}
	}

	payload := map[string]any{
		"company":            []string{company},
		"location":           loc,
		"jobTitle":           []string{"Purchasing", "Procurement", "Buyer", "Owner", "Founder", "Direktur", "Director", "CEO", "General Manager", "Pemilik"},
		"currentTitlesOnly":  true,
		"companyFilter":      "current",
		"seniority":          []string{"Owner / Founder", "CXO", "Director", "VP", "Head", "Manager"},
		"jobFunction":        []string{"Purchasing", "Operations", "Business Development", "Leadership", "Sales"},
	}

	raw, err := leadContactDo(ctx, http.MethodPost, "/api/rest/employess/query/advanced", payload)
	if err != nil {
		return nil, err
	}

	var parsed leadContactSearchResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Code != 200 {
		return nil, fmt.Errorf("leadcontact search: %s (%d)", firstNonEmpty(parsed.Msg, "error"), parsed.Code)
	}

	var out []DecisionMaker
	for _, e := range parsed.Data.Employees {
		name := strings.TrimSpace(e.FullName)
		if !IsValidPersonName(name) {
			continue
		}
		dm := DecisionMaker{
			Name:       name,
			Title:      strings.TrimSpace(e.Title),
			Location:   firstNonEmpty(e.Location, e.Country),
			LinkedIn:   strings.TrimSpace(e.LinkedInURL),
			Avatar:     strings.TrimSpace(e.ProfilePictureURL),
			Source:     "leadcontact.ai",
			Evidence:   "employee search: " + firstNonEmpty(e.CompanyName, company),
			Confidence: "high",
		}
		out = append(out, dm)
		if len(out) >= 8 {
			break
		}
	}
	return out, nil
}

// enrichLeadContactContacts 对已有 LinkedIn /in/ 的决策人补邮箱+电话（按次计费，慎用）。
func enrichLeadContactContacts(ctx context.Context, makers []DecisionMaker) {
	if !LeadContactEnabled() {
		return
	}
	for i := range makers {
		li := strings.TrimSpace(makers[i].LinkedIn)
		if !strings.Contains(strings.ToLower(li), "linkedin.com/in/") {
			continue
		}
		// 资料（含真头像）5 credits
		if makers[i].Avatar == "" || makers[i].Headline == "" {
			if prof, err := leadContactProfile(ctx, li); err == nil {
				if makers[i].Avatar == "" {
					makers[i].Avatar = strings.TrimSpace(prof.ProfilePictureURL)
				}
				if makers[i].Title == "" {
					makers[i].Title = strings.TrimSpace(prof.Title)
				}
				if makers[i].Location == "" {
					makers[i].Location = firstNonEmpty(prof.Location, prof.Country)
				}
			}
		}
		if makers[i].Email == "" {
			if email, err := leadContactEmail(ctx, li); err == nil && email != "" {
				makers[i].Email = email
			}
		}
		if makers[i].Phone == "" {
			if phone, err := leadContactPhone(ctx, li); err == nil && phone != "" {
				makers[i].Phone = phone
				if looksLikeWhatsApp(phone) {
					makers[i].WhatsApp = phone
				}
			}
		}
	}
}

func leadContactProfile(ctx context.Context, linkedInURL string) (*leadContactEmployee, error) {
	q := "/api/rest/employess/query/linkedin?linkedin_url=" + url.QueryEscape(linkedInURL)
	raw, err := leadContactDo(ctx, http.MethodGet, q, nil)
	if err != nil {
		return nil, err
	}
	var parsed leadContactProfileResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Code != 200 {
		return nil, fmt.Errorf("%s (%d)", parsed.Msg, parsed.Code)
	}
	return &parsed.Data, nil
}

func leadContactEmail(ctx context.Context, linkedInURL string) (string, error) {
	raw, err := leadContactDo(ctx, http.MethodPost, "/api/rest/email/query", map[string]string{
		"profileUrl": linkedInURL,
	})
	if err != nil {
		return "", err
	}
	var parsed leadContactContactResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 200 {
		return "", fmt.Errorf("%s (%d)", parsed.Msg, parsed.Code)
	}
	for _, s := range parsed.Data.Sources {
		if s.Email != "" && (s.Valid || true) {
			return s.Email, nil
		}
	}
	return "", fmt.Errorf("no email")
}

func leadContactPhone(ctx context.Context, linkedInURL string) (string, error) {
	raw, err := leadContactDo(ctx, http.MethodPost, "/api/rest/phone/query", map[string]string{
		"profileUrl": linkedInURL,
	})
	if err != nil {
		return "", err
	}
	var parsed leadContactContactResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Code != 200 {
		return "", fmt.Errorf("%s (%d)", parsed.Msg, parsed.Code)
	}
	for _, s := range parsed.Data.Sources {
		if s.Phone != "" {
			return s.Phone, nil
		}
	}
	return "", fmt.Errorf("no phone")
}
