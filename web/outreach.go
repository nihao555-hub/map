package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/outreach"
)

// formOn is the value browsers submit for checked checkboxes.
const formOn = "on"

const outreachTestTimeout = 45 * time.Second

type outreachPageData struct {
	Campaigns []outreach.CampaignView
	Jobs      []Job
	Settings  outreach.SettingsView
	Providers []outreach.Provider
	Sequence  []outreach.SequenceStep
	Panel     string
	Notice    string
	Error     string
}

func (s *Server) outreachPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	if s.outreach == nil {
		http.Error(w, "Outreach module is unavailable", http.StatusServiceUnavailable)

		return
	}

	data, err := s.loadOutreachPage(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	data.Notice = r.URL.Query().Get("notice")
	data.Error = r.URL.Query().Get("error")
	data.Panel = r.URL.Query().Get("panel")

	if data.Panel == "" {
		data.Panel = "workspace"
	}

	tmpl, ok := s.tmpl["static/templates/outreach.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render outreach page: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) loadOutreachPage(ctx context.Context) (outreachPageData, error) {
	campaigns, err := s.outreach.Campaigns(ctx)
	if err != nil {
		return outreachPageData{}, err
	}

	jobs, err := s.svc.All(ctx)
	if err != nil {
		return outreachPageData{}, err
	}

	eligibleJobs := make([]Job, 0, len(jobs))

	for i := range jobs {
		if jobs[i].Status == StatusOK && jobs[i].Data.Email {
			eligibleJobs = append(eligibleJobs, jobs[i])
		}
	}

	settings, err := s.outreach.Settings(ctx)
	if err != nil {
		return outreachPageData{}, err
	}

	return outreachPageData{
		Campaigns: campaigns,
		Jobs:      eligibleJobs,
		Settings:  settings,
		Providers: outreach.Providers(),
		Sequence:  outreach.DefaultSequence(),
	}, nil
}

func (s *Server) outreachSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "settings", "", err)

		return
	}

	current, err := s.outreach.Settings(r.Context())
	if err != nil {
		redirectOutreach(w, r, "settings", "", err)

		return
	}

	settings := current.Settings
	settings.EmailAddress = strings.TrimSpace(r.Form.Get("email_address"))
	settings.Password = r.Form.Get("password")
	settings.FromName = strings.TrimSpace(r.Form.Get("from_name"))
	settings.SenderCompany = strings.TrimSpace(r.Form.Get("sender_company"))
	settings.SMTPHost = strings.TrimSpace(r.Form.Get("smtp_host"))
	settings.SMTPPort = formInt(r, "smtp_port", settings.SMTPPort)
	settings.SMTPTLS = strings.TrimSpace(r.Form.Get("smtp_tls"))
	settings.IMAPHost = strings.TrimSpace(r.Form.Get("imap_host"))
	settings.IMAPPort = formInt(r, "imap_port", settings.IMAPPort)
	settings.SendStartHour = formInt(r, "send_start_hour", settings.SendStartHour)
	settings.SendEndHour = formInt(r, "send_end_hour", settings.SendEndHour)
	settings.SendDays = strings.TrimSpace(r.Form.Get("send_days"))
	settings.DailyCap = formInt(r, "daily_cap", settings.DailyCap)
	settings.WarmupStart = formInt(r, "warmup_start", settings.WarmupStart)
	settings.WarmupStep = formInt(r, "warmup_step", settings.WarmupStep)
	settings.MinGapSeconds = formInt(r, "min_gap_seconds", settings.MinGapSeconds)
	settings.MaxGapSeconds = formInt(r, "max_gap_seconds", settings.MaxGapSeconds)
	settings.DefaultTimezone = strings.TrimSpace(r.Form.Get("default_timezone"))
	settings.UnsubscribeText = strings.TrimSpace(r.Form.Get("unsubscribe_text"))
	settings.AIBaseURL = strings.TrimSpace(r.Form.Get("ai_base_url"))
	settings.AIModel = strings.TrimSpace(r.Form.Get("ai_model"))
	settings.AIAPIKey = strings.TrimSpace(r.Form.Get("ai_api_key"))

	if err := s.outreach.SaveSettings(r.Context(), &settings); err != nil {
		redirectOutreach(w, r, "settings", "", err)

		return
	}

	redirectOutreach(w, r, "settings", "设置已保存；授权码与 AI 密钥只保存在本进程内存中", nil)
}

func (s *Server) outreachTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), outreachTestTimeout)
	defer cancel()

	if err := s.outreach.TestConnections(ctx); err != nil {
		redirectOutreach(w, r, "settings", "", fmt.Errorf("连接测试失败: %w", err))

		return
	}

	redirectOutreach(w, r, "settings", "SMTP 和 IMAP 连接、认证均成功（未发送测试邮件）", nil)
}

func (s *Server) outreachCampaigns(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "campaigns", "", err)

		return
	}

	input := outreach.CampaignInput{
		Name:             r.Form.Get("name"),
		JobID:            r.Form.Get("job_id"),
		ValueProposition: r.Form.Get("value_proposition"),
		Proof:            r.Form.Get("proof"),
		CallToAction:     r.Form.Get("call_to_action"),
		Start:            r.Form.Get("start_now") == formOn,
	}

	campaign, summary, err := s.outreach.CreateCampaignFromJob(r.Context(), &input)
	if err != nil {
		redirectOutreach(w, r, "campaigns", "", err)

		return
	}

	if input.Start {
		if err := s.outreach.SetCampaignStatus(r.Context(), campaign.ID, outreach.CampaignStatusActive); err != nil {
			redirectOutreach(w, r, "campaigns", "", err)

			return
		}
	}

	notice := fmt.Sprintf("已创建活动并导入 %d 个有效客户（每个商户只保留一个最优邮箱）", summary.Inserted)
	redirectOutreach(w, r, "campaigns", notice, nil)
}

func (s *Server) outreachCampaign(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		http.Error(w, "Outreach module is unavailable", http.StatusServiceUnavailable)

		return
	}

	id := r.PathValue("id")

	switch r.Method {
	case http.MethodGet:
		http.Redirect(
			w,
			r,
			"/outreach?panel=workspace&campaign="+url.QueryEscape(id),
			http.StatusSeeOther,
		)
	case http.MethodDelete, http.MethodPost:
		if err := s.outreach.DeleteCampaign(r.Context(), id); err != nil {
			redirectOutreach(w, r, "campaigns", "", err)

			return
		}

		redirectOutreach(w, r, "campaigns", "活动已删除", nil)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) outreachCampaignStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "campaigns", "", err)

		return
	}

	id := r.PathValue("id")
	status := r.Form.Get("status")

	if err := s.outreach.SetCampaignStatus(r.Context(), id, status); err != nil {
		redirectOutreach(w, r, "campaigns", "", err)

		return
	}

	redirectOutreach(w, r, "campaigns", "活动状态已更新", nil)
}

func (s *Server) outreachTick(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	report, err := s.outreach.Tick(r.Context())
	if err != nil {
		redirectOutreach(w, r, "workspace", "", err)

		return
	}

	notice := fmt.Sprintf(
		"执行完成：发送 %d，回复 %d，退信 %d，退订 %d；%s",
		report.Sent,
		report.Replies,
		report.Bounces,
		report.Unsubscribed,
		report.State,
	)
	redirectOutreach(w, r, "workspace", notice, nil)
}

// apiOutreachContacts serves the workspace contact list as JSON.
func (s *Server) apiOutreachContacts(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		renderOutreachAPIError(w, errors.New("outreach module unavailable"))

		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	filter := outreach.WorkspaceFilter{
		CampaignID: r.URL.Query().Get("campaign"),
		Status:     r.URL.Query().Get("status"),
		Search:     strings.TrimSpace(r.URL.Query().Get("q")),
	}

	contacts, err := s.outreach.WorkspaceContacts(r.Context(), filter)
	if err != nil {
		renderOutreachAPIError(w, err)

		return
	}

	if contacts == nil {
		contacts = []outreach.WorkspaceContact{}
	}

	renderJSON(w, http.StatusOK, contacts)
}

// apiOutreachContact serves one contact's conversation thread.
func (s *Server) apiOutreachContact(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		renderOutreachAPIError(w, errors.New("outreach module unavailable"))

		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	contactID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		renderOutreachAPIError(w, errors.New("invalid contact ID"))

		return
	}

	thread, err := s.outreach.ContactThread(r.Context(), contactID)
	if err != nil {
		renderOutreachAPIError(w, err)

		return
	}

	if thread.Messages == nil {
		thread.Messages = []outreach.Message{}
	}

	renderJSON(w, http.StatusOK, thread)
}

// apiOutreachSuggest drafts an AI reply for one contact.
func (s *Server) apiOutreachSuggest(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	contactID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		renderOutreachAPIError(w, errors.New("invalid contact ID"))

		return
	}

	suggestion, err := s.outreach.SuggestReply(r.Context(), contactID)
	if err != nil {
		renderOutreachAPIError(w, err)

		return
	}

	renderJSON(w, http.StatusOK, map[string]string{"suggestion": suggestion})
}

func (s *Server) apiOutreachCampaigns(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		renderJSON(w, http.StatusServiceUnavailable, apiError{
			Code:    http.StatusServiceUnavailable,
			Message: "outreach module unavailable",
		})

		return
	}

	switch r.Method {
	case http.MethodGet:
		campaigns, err := s.outreach.Campaigns(r.Context())
		if err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		renderJSON(w, http.StatusOK, campaigns)
	case http.MethodPost:
		var request outreach.CampaignInput

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			renderJSON(w, http.StatusUnprocessableEntity, apiError{
				Code:    http.StatusUnprocessableEntity,
				Message: err.Error(),
			})

			return
		}

		campaign, summary, err := s.outreach.CreateCampaignFromJob(r.Context(), &request)
		if err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		if request.Start {
			if err := s.outreach.SetCampaignStatus(
				r.Context(),
				campaign.ID,
				outreach.CampaignStatusActive,
			); err != nil {
				renderOutreachAPIError(w, err)

				return
			}

			campaign.Status = outreach.CampaignStatusActive
		}

		renderJSON(w, http.StatusCreated, map[string]any{
			"campaign": campaign,
			"import":   summary,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) apiOutreachCampaign(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		renderOutreachAPIError(w, errors.New("outreach module unavailable"))

		return
	}

	id := r.PathValue("id")

	switch r.Method {
	case http.MethodGet:
		campaign, err := s.outreach.Campaign(r.Context(), id)
		if err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		contacts, err := s.outreach.Contacts(r.Context(), id, 500)
		if err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		renderJSON(w, http.StatusOK, map[string]any{
			"campaign": campaign,
			"contacts": contacts,
		})
	case http.MethodDelete:
		if err := s.outreach.DeleteCampaign(r.Context(), id); err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) apiOutreachSettings(w http.ResponseWriter, r *http.Request) {
	if s.outreach == nil {
		renderOutreachAPIError(w, errors.New("outreach module unavailable"))

		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := s.outreach.Settings(r.Context())
		if err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		renderJSON(w, http.StatusOK, settings)
	case http.MethodPost:
		var settings outreach.Settings

		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			renderJSON(w, http.StatusUnprocessableEntity, apiError{
				Code:    http.StatusUnprocessableEntity,
				Message: err.Error(),
			})

			return
		}

		if err := s.outreach.SaveSettings(r.Context(), &settings); err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) apiOutreachTick(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	report, err := s.outreach.Tick(r.Context())
	if err != nil {
		renderOutreachAPIError(w, err)

		return
	}

	renderJSON(w, http.StatusOK, report)
}

func (s *Server) apiOutreachReply(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachPost(w, r) {
		return
	}

	var request struct {
		ContactID int64  `json:"contact_id"`
		Body      string `json:"body"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		})

		return
	}

	message, err := s.outreach.Reply(r.Context(), request.ContactID, request.Body)
	if err != nil {
		renderOutreachAPIError(w, err)

		return
	}

	renderJSON(w, http.StatusCreated, message)
}

func (s *Server) requireOutreachPost(w http.ResponseWriter, r *http.Request) bool {
	if s.outreach == nil {
		http.Error(w, "Outreach module is unavailable", http.StatusServiceUnavailable)

		return false
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return false
	}

	return true
}

func formInt(r *http.Request, name string, fallback int) int {
	value := strings.TrimSpace(r.Form.Get(name))
	if value == "" {
		return fallback
	}

	result, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return result
}

func redirectOutreach(w http.ResponseWriter, r *http.Request, panel, notice string, err error) {
	values := url.Values{}

	if panel != "" {
		values.Set("panel", panel)
	}

	if err != nil {
		values.Set("error", err.Error())
	} else if notice != "" {
		values.Set("notice", notice)
	}

	target := "/outreach"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

func renderOutreachAPIError(w http.ResponseWriter, err error) {
	code := http.StatusUnprocessableEntity
	if errors.Is(err, outreach.ErrNotFound) {
		code = http.StatusNotFound
	}

	renderJSON(w, code, apiError{
		Code:    code,
		Message: err.Error(),
	})
}
