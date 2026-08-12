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

type outreachPageData struct {
	Campaigns []outreach.CampaignView
	Jobs      []Job
	Settings  outreach.SettingsView
	Messages  []outreach.Message
	Contacts  []outreach.Contact
	Selected  *outreach.CampaignView
	Providers []outreach.Provider
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

	data, err := s.loadOutreachPage(r.Context(), r.URL.Query().Get("campaign"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	data.Notice = r.URL.Query().Get("notice")
	data.Error = r.URL.Query().Get("error")

	tmpl, ok := s.tmpl["static/templates/outreach.html"]
	if !ok {
		http.Error(w, "missing tpl", http.StatusInternalServerError)

		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render outreach page: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) loadOutreachPage(ctx context.Context, selectedID string) (outreachPageData, error) {
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

	messages, err := s.outreach.Messages(ctx, "", 50)
	if err != nil {
		return outreachPageData{}, err
	}

	data := outreachPageData{
		Campaigns: campaigns,
		Jobs:      eligibleJobs,
		Settings:  settings,
		Messages:  messages,
		Providers: outreach.Providers(),
	}

	if selectedID == "" {
		return data, nil
	}

	selected, err := s.outreach.Campaign(ctx, selectedID)
	if err != nil {
		return outreachPageData{}, err
	}

	contacts, err := s.outreach.Contacts(ctx, selectedID, 500)
	if err != nil {
		return outreachPageData{}, err
	}

	data.Selected = &selected
	data.Contacts = contacts

	return data, nil
}

func (s *Server) outreachSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	current, err := s.outreach.Settings(r.Context())
	if err != nil {
		redirectOutreach(w, r, "", err)

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

	if err := s.outreach.SaveSettings(r.Context(), settings); err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	redirectOutreach(w, r, "邮箱设置已保存；授权码只保存在本进程内存中", nil)
}

func (s *Server) outreachTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	if err := s.outreach.TestConnections(ctx); err != nil {
		redirectOutreach(w, r, "", fmt.Errorf("连接测试失败: %w", err))

		return
	}

	redirectOutreach(w, r, "SMTP 和 IMAP 连接、认证均成功（未发送测试邮件）", nil)
}

func (s *Server) outreachCampaigns(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	campaign, summary, err := s.outreach.CreateCampaignFromJob(
		r.Context(),
		outreach.CampaignInput{
			Name:             r.Form.Get("name"),
			JobID:            r.Form.Get("job_id"),
			ValueProposition: r.Form.Get("value_proposition"),
			Proof:            r.Form.Get("proof"),
			CallToAction:     r.Form.Get("call_to_action"),
			Start:            r.Form.Get("start_now") == "on",
		},
	)
	if err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	if r.Form.Get("start_now") == "on" {
		if err := s.outreach.SetCampaignStatus(r.Context(), campaign.ID, outreach.CampaignStatusActive); err != nil {
			redirectOutreach(w, r, "", err)

			return
		}
	}

	notice := fmt.Sprintf("已创建活动并导入 %d 个有效邮箱", summary.Inserted)
	redirectOutreachToCampaign(w, r, campaign.ID, notice, nil)
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
			"/outreach?campaign="+url.QueryEscape(id),
			http.StatusSeeOther,
		)
	case http.MethodDelete, http.MethodPost:
		if err := s.outreach.DeleteCampaign(r.Context(), id); err != nil {
			redirectOutreach(w, r, "", err)

			return
		}

		redirectOutreach(w, r, "活动已删除", nil)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) outreachCampaignStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	id := r.PathValue("id")
	status := r.Form.Get("status")

	if err := s.outreach.SetCampaignStatus(r.Context(), id, status); err != nil {
		redirectOutreachToCampaign(w, r, id, "", err)

		return
	}

	redirectOutreachToCampaign(w, r, id, "活动状态已更新", nil)
}

func (s *Server) outreachTick(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	report, err := s.outreach.Tick(r.Context())
	if err != nil {
		redirectOutreach(w, r, "", err)

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
	redirectOutreach(w, r, notice, nil)
}

func (s *Server) outreachReply(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	contactID, err := strconv.ParseInt(r.Form.Get("contact_id"), 10, 64)
	if err != nil {
		redirectOutreach(w, r, "", errors.New("invalid contact ID"))

		return
	}

	message, err := s.outreach.Reply(r.Context(), contactID, r.Form.Get("body"))
	if err != nil {
		redirectOutreach(w, r, "", err)

		return
	}

	redirectOutreachToCampaign(w, r, message.CampaignID, "回信已在原邮件会话中发出", nil)
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

		campaign, summary, err := s.outreach.CreateCampaignFromJob(
			r.Context(),
			request,
		)
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

		if err := s.outreach.SaveSettings(r.Context(), settings); err != nil {
			renderOutreachAPIError(w, err)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) apiOutreachTick(w http.ResponseWriter, r *http.Request) {
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
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
	if !s.requireOutreachMethod(w, r, http.MethodPost) {
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

func (s *Server) requireOutreachMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if s.outreach == nil {
		http.Error(w, "Outreach module is unavailable", http.StatusServiceUnavailable)

		return false
	}

	if r.Method != method {
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

func redirectOutreach(w http.ResponseWriter, r *http.Request, notice string, err error) {
	redirectOutreachToCampaign(w, r, "", notice, err)
}

func redirectOutreachToCampaign(
	w http.ResponseWriter,
	r *http.Request,
	campaignID string,
	notice string,
	err error,
) {
	values := url.Values{}
	if campaignID != "" {
		values.Set("campaign", campaignID)
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
