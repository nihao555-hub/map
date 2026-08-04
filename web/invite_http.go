package web

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) inviteRequired() bool {
	return s.invites != nil && s.inviteGate
}

func (s *Server) inviteGateMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.inviteRequired() {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		if invitePublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if s.hasValidInviteSession(r) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(path, "/api/") {
			renderJSON(w, http.StatusUnauthorized, apiError{
				Code:    http.StatusUnauthorized,
				Message: "需要邀请码：请先访问 /invite 兑换，或携带有效会话 Cookie",
			})
			return
		}

		nextURL := path
		if r.URL.RawQuery != "" {
			nextURL += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, "/invite?next="+url.QueryEscape(nextURL), http.StatusFound)
	})
}

func invitePublicPath(path string) bool {
	switch {
	case path == "/invite", path == "/invite/":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	default:
		return false
	}
}

func (s *Server) hasValidInviteSession(r *http.Request) bool {
	token := inviteTokenFromRequest(r)
	if token == "" || s.invites == nil {
		return false
	}
	ok, err := s.invites.ValidSession(r.Context(), token)
	if err != nil {
		log.Printf("invite session check: %v", err)
		return false
	}
	return ok
}

func inviteTokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(InviteCookieName); err == nil && c != nil {
		if t := strings.TrimSpace(c.Value); t != "" {
			return t
		}
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if t := strings.TrimSpace(r.Header.Get("X-Invite-Session")); t != "" {
		return t
	}
	return ""
}

func (s *Server) invitePage(w http.ResponseWriter, r *http.Request) {
	if !s.inviteRequired() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if r.Method == http.MethodGet && s.hasValidInviteSession(r) {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.renderInvite(w, invitePageData{
			Next: safeNext(r.URL.Query().Get("next")),
		})
	case http.MethodPost:
		s.redeemInvite(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type invitePageData struct {
	Error string
	Next  string
}

func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderInvite(w, invitePageData{Error: "表单无效，请重试", Next: "/"})
		return
	}

	code := strings.TrimSpace(r.FormValue("code"))
	next := safeNext(r.FormValue("next"))
	if code == "" {
		s.renderInvite(w, invitePageData{Error: "请输入邀请码", Next: next})
		return
	}

	token, expires, err := s.invites.Redeem(r.Context(), code)
	if err != nil {
		msg := "邀请码无效或已被使用"
		if !errors.Is(err, ErrInvalidInvite) {
			log.Printf("invite redeem: %v", err)
			msg = "兑换失败，请稍后重试"
		}
		s.renderInvite(w, invitePageData{Error: msg, Next: next})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     InviteCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})

	http.Redirect(w, r, next, http.StatusFound)
}

func (s *Server) renderInvite(w http.ResponseWriter, data invitePageData) {
	tmpl := s.tmpl["static/templates/invite.html"]
	if tmpl == nil {
		http.Error(w, "invite template missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("invite template: %v", err)
	}
}

func safeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	if u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	if u.Path == "/invite" || strings.HasPrefix(u.Path, "/invite/") {
		return "/"
	}
	out := u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}
