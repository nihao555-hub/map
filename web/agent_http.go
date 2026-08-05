package web

import (
	"context"
	"encoding/json"
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
	plan := PlanTasks(intent)
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

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
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

	plan := PlanTasks(intent)
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
	renderJSON(w, http.StatusCreated, result)
}
