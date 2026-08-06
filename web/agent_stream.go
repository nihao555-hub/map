package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type agentStreamEvent struct {
	Type     string          `json:"type"` // thinking_delta | status | result | error | done
	Text     string          `json:"text,omitempty"`
	Message  string          `json:"message,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
	Plan     *AgentPlan      `json:"plan,omitempty"`
	JobIDs   []string        `json:"job_ids,omitempty"`
	Tools    []AgentToolCall `json:"tools,omitempty"`
	Model    string          `json:"model,omitempty"`
	Source   string          `json:"source,omitempty"`
}

// apiAgentDispatchStream streams user-facing thinking tokens (SSE), then the final plan/jobs.
func (s *Server) apiAgentDispatchStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		renderJSON(w, http.StatusMethodNotAllowed, apiError{Code: http.StatusMethodNotAllowed, Message: "Method not allowed"})
		return
	}
	if s.inviteRequired() && s.requestOwner(r) == "" {
		renderJSON(w, http.StatusUnauthorized, apiError{Code: http.StatusUnauthorized, Message: "需要有效邀请会话"})
		return
	}
	if !AITranslateEnabled() {
		renderJSON(w, http.StatusServiceUnavailable, apiError{
			Code:    http.StatusServiceUnavailable,
			Message: "未配置 GRSAI_API_KEY，无法使用真实 AI 流式输出",
		})
		return
	}

	var req agentDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: "invalid JSON"})
		return
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		renderJSON(w, http.StatusBadRequest, apiError{Code: http.StatusBadRequest, Message: "empty goal"})
		return
	}
	uiLang := normalizeUILang(req.UILang)
	owner := s.requestOwner(r)

	flusher, ok := w.(http.Flusher)
	if !ok {
		renderJSON(w, http.StatusInternalServerError, apiError{Code: http.StatusInternalServerError, Message: "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	events := make(chan agentStreamEvent, 64)
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for ev := range events {
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}()

	send := func(ev agentStreamEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}

	send(agentStreamEvent{Type: "status", Text: "正在连接模型…"})

	var (
		mu       sync.Mutex
		thinking strings.Builder
		intent   AgentIntent
		plan     AgentPlan
		dispatch AgentDispatchResult
		pipeErr  error
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		err := streamAgentThinking(ctx, goal, uiLang, func(delta string) {
			mu.Lock()
			thinking.WriteString(delta)
			mu.Unlock()
			send(agentStreamEvent{Type: "thinking_delta", Text: delta})
		})
		if err != nil {
			log.Printf("agent thinking stream: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		send(agentStreamEvent{Type: "status", Text: "正在理解目标并拆分抓取任务…"})
		var err error
		intent, err = UnderstandIntent(ctx, goal, uiLang)
		if err != nil {
			pipeErr = err
			return
		}
		if intent.Source != "ai" {
			pipeErr = fmt.Errorf("AI 未生效（当前为规则回退），请检查 GRSAI_API_KEY")
			return
		}
		plan = PlanTasksAI(ctx, intent)
		if len(plan.Tasks) == 0 {
			pipeErr = fmt.Errorf("无法理解目标：请说明「在哪里」和「找什么」")
			return
		}
		send(agentStreamEvent{Type: "status", Text: fmt.Sprintf("已规划 %d 个子任务，正在创建抓取…", len(plan.Tasks))})
		dispatch, err = s.DispatchPlan(ctx, owner, plan)
		if err != nil {
			pipeErr = err
			return
		}
		dispatch.Model = grsaiModel()
		dispatch.Source = intent.Source
		dispatch.Plan = plan
		dispatch.Tools = buildAgentTools(plan, dispatch.JobIDs)
		dispatch.Message = buildUserMessage(plan, dispatch.JobIDs)
	}()

	wg.Wait()

	if pipeErr != nil {
		send(agentStreamEvent{Type: "error", Text: pipeErr.Error()})
		send(agentStreamEvent{Type: "done"})
		close(events)
		writerWG.Wait()
		return
	}

	mu.Lock()
	streamed := strings.TrimSpace(thinking.String())
	mu.Unlock()
	finalThinking := streamed
	if finalThinking == "" {
		finalThinking = buildUserThinking(plan)
	}
	dispatch.Thinking = finalThinking

	send(agentStreamEvent{
		Type:     "result",
		Message:  dispatch.Message,
		Thinking: dispatch.Thinking,
		Plan:     &dispatch.Plan,
		JobIDs:   dispatch.JobIDs,
		Tools:    dispatch.Tools,
		Model:    dispatch.Model,
		Source:   dispatch.Source,
	})
	send(agentStreamEvent{Type: "done"})
	close(events)
	writerWG.Wait()
}

func streamAgentThinking(ctx context.Context, goal, uiLang string, onDelta func(string)) error {
	key := grsaiAPIKey()
	if key == "" {
		return fmt.Errorf("no AI key")
	}
	system := `你是地图获客助手。请用用户界面语言，自然地流式说出你的思考过程：
- 说明你如何理解用户目标
- 说明为什么要拆成多个区域/品类去抓
- 说明抓完后会汇总成表并自动背调
禁止输出 JSON、字段名、API、模型名、UUID、IntentAgent 等技术词。短段落即可，口语化。`
	if uiLang == "en" {
		system = `You are a map lead-gen assistant. Stream your thinking in plain English:
explain the goal, why you split into multiple area/category scrapes, and that results land in a summary table with auto background checks.
No JSON, no API/model names, no UUIDs, no agent role names.`
	}

	body, err := json.Marshal(aiChatRequest{
		Model:  grsaiModel(),
		Stream: true,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: "用户目标：\n" + goal},
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grsaiHost()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("thinking stream status %d: %s", resp.StatusCode, truncate(string(raw), 180))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta != "" {
			onDelta(delta)
		}
	}
	return scanner.Err()
}
