package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type agentUnderstandRequest struct {
	Goal   string `json:"goal"`
	UILang string `json:"ui_lang"`
}

type agentDispatchRequest struct {
	Goal   string       `json:"goal"`
	UILang string       `json:"ui_lang"`
	Intent *AgentIntent `json:"intent,omitempty"` // optional pre-parsed
}

func (s *Server) apiConcurrency(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	renderJSON(w, http.StatusOK, GetConcurrencySnapshot())
}

// apiAgentUnderstand — IntentAgent only (preview, no job create).
func (s *Server) apiAgentUnderstand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	var req agentUnderstandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: "invalid JSON"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	intent, err := UnderstandIntent(ctx, req.Goal, req.UILang)
	if err != nil {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: err.Error()})
		return
	}
	plan := PlanTasksAI(ctx, intent)
	renderJSON(w, http.StatusOK, plan)
}

// apiAgentDispatch — full pipeline: Intent → Planner → Localizer → Dispatcher.
func (s *Server) apiAgentDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	var req agentDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: "invalid JSON"})
		return
	}

	if s.inviteRequired() && s.requestOwner(r) == "" {
		renderJSON(w, http.StatusUnauthorized, apiError{Code: http.StatusUnauthorized, Message: "需要有效邀请会话"})
		return
	}
	owner := s.requestOwner(r)

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	var intent AgentIntent
	var err error
	if req.Intent != nil && len(req.Intent.Keywords) > 0 {
		intent = normalizeIntent(*req.Intent, strings.TrimSpace(req.Goal), req.UILang)
		intent.Source = "client"
	} else {
		intent, err = UnderstandIntent(ctx, req.Goal, req.UILang)
		if err != nil {
			renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: err.Error()})
			return
		}
	}

	plan := PlanTasksAI(ctx, intent)
	if len(plan.Tasks) == 0 {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "无法理解目标：请说明「在哪里」和「找什么」，例如：在雅加达找咖啡馆，半径15公里",
		})
		return
	}
	for _, t := range plan.Tasks {
		if strings.TrimSpace(t.Location) == "" && strings.TrimSpace(t.CountryCode) == "" {
			renderJSON(w, http.StatusUnprocessableEntity, apiError{
				Code:    http.StatusUnprocessableEntity,
				Message: "缺少地点或国家：自然语言里需要可识别的城市/国家",
			})
			return
		}
	}

	result, err := s.DispatchPlan(ctx, owner, plan)
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: err.Error()})
		return
	}
	result.Model = grsaiModel()
	result.Source = intent.Source
	result.Thinking = buildUserThinking(plan)
	result.Steps = buildAgentPipelineSteps(plan, result.JobIDs)
	result.Tools = buildAgentTools(plan, result.JobIDs)
	result.Message = buildUserMessage(plan, result.JobIDs)
	renderJSON(w, http.StatusCreated, result)
}

func buildUserThinking(plan AgentPlan) string {
	if t := strings.TrimSpace(plan.Intent.Thinking); t != "" {
		return t
	}
	in := plan.Intent
	kw := strings.Join(in.Keywords, "、")
	if kw == "" {
		kw = "目标商家"
	}
	loc := in.Location
	if loc == "" {
		loc = in.CountryName
	}
	if len(plan.Tasks) > 1 {
		return fmt.Sprintf(
			"你想在「%s」找「%s」。区域覆盖面比较大，我会拆成 %d 个子任务分区域深度抓取，并把结果汇总到一张表里；商家出现后会自动开始背调。",
			loc, kw, len(plan.Tasks),
		)
	}
	return fmt.Sprintf(
		"你想在「%s」找「%s」。我会按深度全量方式抓取，结果汇总成表格，商家出现后自动背调。",
		loc, kw,
	)
}

func buildUserMessage(plan AgentPlan, jobIDs []string) string {
	n := len(jobIDs)
	if n == 0 {
		n = len(plan.Tasks)
	}
	if n > 1 {
		return fmt.Sprintf("已启动，拆成 %d 个子任务深度抓取（忙时排队）。下方结果表会持续增长，商家出现后自动背调。", n)
	}
	return "已启动深度全量抓取。下方结果表会持续增长，商家出现后自动背调。"
}

func buildAgentPipelineSteps(plan AgentPlan, jobIDs []string) []AgentPipelineStep {
	in := plan.Intent
	kw := strings.Join(in.Keywords, "、")
	loc := in.Location
	if loc == "" {
		loc = in.CountryName
	}
	taskLabels := make([]string, 0, len(plan.Tasks))
	for _, t := range plan.Tasks {
		taskLabels = append(taskLabels, t.Name)
	}
	return []AgentPipelineStep{
		{
			ID: "intent", Title: "理解需求", Status: "complete",
			Summary: fmt.Sprintf("在「%s」寻找「%s」，尽量找全", loc, kw),
		},
		{
			ID: "plan", Title: "拆分子任务", Status: "complete",
			Summary: fmt.Sprintf("拆成 %d 项：%s", len(plan.Tasks), strings.Join(taskLabels, "；")),
		},
		{
			ID: "localize", Title: "本地化搜索词", Status: "complete",
			Summary: "把关键词翻译/对齐到当地地图可搜的说法，并锚定搜索中心",
		},
		{
			ID: "dispatch", Title: "创建抓取任务", Status: "complete",
			Summary: fmt.Sprintf("已创建 %d 个抓取任务（忙时排队，执行中保持满速）", len(jobIDs)),
		},
		{
			ID: "scrape", Title: "深度抓取中", Status: "active",
			Summary: "结果会持续写入汇总表；出现商家后自动开始背调",
		},
	}
}

func buildAgentTools(plan AgentPlan, jobIDs []string) []AgentToolCall {
	in := plan.Intent
	kw := strings.Join(in.Keywords, "、")
	loc := in.Location
	if loc == "" {
		loc = in.CountryName
	}
	taskNames := make([]string, 0, len(plan.Tasks))
	for _, t := range plan.Tasks {
		taskNames = append(taskNames, t.Name)
	}
	return []AgentToolCall{
		{
			Name: "understand_goal", Title: "理解用户目标", Status: "complete",
			Input:  map[string]any{"地点": loc, "品类": kw},
			Output: fmt.Sprintf("已理解：在 %s 找 %s", loc, kw),
		},
		{
			Name: "plan_subtasks", Title: "规划抓取子任务", Status: "complete",
			Input:  map[string]any{"子任务数": len(plan.Tasks)},
			Output: strings.Join(taskNames, "\n"),
		},
		{
			Name: "localize_keywords", Title: "本地化关键词", Status: "complete",
			Input:  map[string]any{"原始品类": kw},
			Output: "已生成本地可搜关键词并完成坐标锚定",
		},
		{
			Name: "dispatch_scrape", Title: "派发地图抓取", Status: "complete",
			Input:  map[string]any{"任务数": len(jobIDs)},
			Output: fmt.Sprintf("已派发 %d 个深度全量抓取任务", len(jobIDs)),
		},
		{
			Name: "collect_results", Title: "汇总结果与背调", Status: "running",
			Input:  map[string]any{"说明": "结果出现后自动背调"},
			Output: "抓取进行中，表格将持续更新",
		},
	}
}

type jobQueueResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Phase        string `json:"phase,omitempty"`
	Ahead        int    `json:"ahead"`
	PendingTotal int    `json:"pending_total"`
	ActiveJobs   int    `json:"active_jobs"`
	AdmitSlots   int    `json:"admit_slots"`
	Message      string `json:"message"`
}

func (s *Server) apiJobQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	id, ok := getIDFromRequest(r)
	if !ok {
		renderJSON(w, http.StatusUnprocessableEntity, apiError{Code: http.StatusUnprocessableEntity, Message: "Invalid ID"})
		return
	}
	job, err := s.loadAccessibleJob(r, id.String())
	if err != nil {
		renderJSON(w, http.StatusNotFound, apiError{Code: http.StatusNotFound, Message: "Not found"})
		return
	}
	ahead, status, pendingTotal, err := s.svc.QueueAhead(r.Context(), id.String())
	if err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: err.Error()})
		return
	}
	s.svc.EnrichJobPhase(r.Context(), &job)
	snap := GetConcurrencySnapshot()
	msg := queueMessage(job.Phase, status, ahead)
	renderJSON(w, http.StatusOK, jobQueueResponse{
		ID: id.String(), Status: status, Phase: job.Phase, Ahead: ahead, PendingTotal: pendingTotal,
		ActiveJobs: snap.ActiveJobs, AdmitSlots: snap.AdmitSlots, Message: msg,
	})
}

type agentJobsQueueRequest struct {
	JobIDs []string `json:"job_ids"`
}

func (s *Server) apiAgentJobsQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	var req agentJobsQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: "invalid JSON"})
		return
	}
	snap := GetConcurrencySnapshot()
	out := make([]jobQueueResponse, 0, len(req.JobIDs))
	for _, id := range req.JobIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		job, err := s.loadAccessibleJob(r, id)
		if err != nil {
			continue
		}
		ahead, status, pendingTotal, err := s.svc.QueueAhead(r.Context(), id)
		if err != nil {
			continue
		}
		s.svc.EnrichJobPhase(r.Context(), &job)
		out = append(out, jobQueueResponse{
			ID: id, Status: status, Phase: job.Phase, Ahead: ahead, PendingTotal: pendingTotal,
			ActiveJobs: snap.ActiveJobs, AdmitSlots: snap.AdmitSlots,
			Message: queueMessage(job.Phase, status, ahead),
		})
	}
	renderJSON(w, http.StatusOK, map[string]any{"jobs": out, "active_jobs": snap.ActiveJobs, "admit_slots": snap.AdmitSlots})
}

func queueMessage(phase, status string, ahead int) string {
	if phase == PhaseIntel {
		return "采集完成，背调中"
	}
	switch status {
	case StatusPending:
		if ahead <= 0 {
			return "排队中，即将开始采集"
		}
		return fmt.Sprintf("排队中，前面还有 %d 个任务", ahead)
	case StatusWorking:
		return "采集中，结果持续增加"
	case StatusOK:
		return "已完成"
	case StatusFailed:
		return "失败"
	case StatusCanceled:
		return "已终止"
	default:
		return status
	}
}
