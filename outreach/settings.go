package outreach

import (
	"os"
	"strconv"
	"strings"
)

// TLS modes for SMTP.
const (
	TLSModeSSL      = "ssl"      // implicit TLS (port 465/994)
	TLSModeStartTLS = "starttls" // STARTTLS upgrade (port 587/25)
)

// Outbound channels. SMTP sends through the mailbox provider directly; API
// sends through the Brevo transactional HTTP API (free tier: 300 emails/day)
// while replies keep arriving in the mailbox over IMAP.
const (
	SendViaSMTP = "smtp"
	SendViaAPI  = "api"
)

// Settings holds the mailbox configuration and the sending policy.
// It is stored locally in the outreach SQLite database; secrets can instead
// be provided via environment variables (recommended) which always win.
type Settings struct {
	SMTPHost      string `json:"smtp_host"`
	SMTPPort      int    `json:"smtp_port"`
	SMTPTLS       string `json:"smtp_tls"`
	IMAPHost      string `json:"imap_host"`
	IMAPPort      int    `json:"imap_port"`
	EmailAddress  string `json:"email_address"`
	Password      string `json:"password,omitempty"`
	FromName      string `json:"from_name"`
	SenderCompany string `json:"sender_company"`

	// SendVia selects the outbound channel: SendViaSMTP (default) or
	// SendViaAPI. With the API channel the From/Reply-To address stays the
	// mailbox above, so customer replies flow into the same IMAP inbox.
	SendVia string `json:"send_via"`
	// SendAPIKey is the Brevo v3 API key. Like Password it is never
	// persisted; provide it via the UI (in-memory) or the environment.
	SendAPIKey string `json:"send_api_key,omitempty"`

	// Send policy. Times are in the recipient's local timezone.
	SendStartHour int `json:"send_start_hour"`
	SendEndHour   int `json:"send_end_hour"`
	// SendDays holds ISO weekday digits, e.g. "234" = Tue,Wed,Thu.
	SendDays string `json:"send_days"`
	// DailyCap is the hard maximum of cold emails per calendar day.
	DailyCap int `json:"daily_cap"`
	// WarmupStart/WarmupStep ramp the daily volume up slowly so a fresh
	// mailbox builds sender reputation instead of landing in spam.
	WarmupStart int `json:"warmup_start"`
	WarmupStep  int `json:"warmup_step"`
	// MinGapSeconds/MaxGapSeconds is the randomized pause between sends.
	MinGapSeconds int `json:"min_gap_seconds"`
	MaxGapSeconds int `json:"max_gap_seconds"`
	// DefaultTimezone is used when a lead has no timezone information.
	DefaultTimezone string `json:"default_timezone"`
	// UnsubscribeText is appended as a plain-text footer for compliance.
	UnsubscribeText string `json:"unsubscribe_text"`

	// AutoReply enables "autopilot" for inbound replies: when on (and AI is
	// configured), the engine auto-answers safe, non-committal replies and
	// escalates money/commitment/complaint/legal/hot-lead replies to the human
	// "needs attention" queue. Off by default — replies wait for a human.
	AutoReply bool `json:"auto_reply"`

	// AI copywriting through any OpenAI-compatible chat-completions API
	// (DeepSeek, Qwen, Kimi, OpenAI, local Ollama, ...). When configured,
	// outgoing steps are written per contact from the scraped facts plus
	// website research, replies get suggested drafts, and customer intent
	// is scored from reply content. Without it the module falls back to
	// deterministic templates and keyword heuristics.
	AIBaseURL string `json:"ai_base_url"`
	AIModel   string `json:"ai_model"`
	AIAPIKey  string `json:"ai_api_key,omitempty"`
}

// DefaultSettings returns conservative defaults tuned for a fresh mailbox on
// a shared business-mail platform (NetEase/Tencent enterprise mail and the
// like have low daily SMTP quotas and aggressive outbound spam checks).
func DefaultSettings() Settings {
	return Settings{
		SMTPPort:        465,
		SMTPTLS:         TLSModeSSL,
		IMAPPort:        993,
		SendStartHour:   8,
		SendEndHour:     11,
		SendDays:        "234", // Tue, Wed, Thu: the strongest reply days
		DailyCap:        40,
		WarmupStart:     15,
		WarmupStep:      5,
		MinGapSeconds:   90,
		MaxGapSeconds:   150,
		DefaultTimezone: "UTC",
		UnsubscribeText: "If you'd rather not hear from me again, just reply with \"unsubscribe\" and I won't email you anymore.",
	}
}

// Environment variable names for secret/connection overrides.
const (
	EnvSMTPHost  = "OUTREACH_SMTP_HOST"
	EnvSMTPPort  = "OUTREACH_SMTP_PORT"
	EnvIMAPHost  = "OUTREACH_IMAP_HOST"
	EnvIMAPPort  = "OUTREACH_IMAP_PORT"
	EnvEmail     = "OUTREACH_EMAIL"
	EnvFromName  = "OUTREACH_FROM_NAME"
	EnvDisabled  = "OUTREACH_DISABLED"
	EnvAIBaseURL = "OUTREACH_AI_BASE_URL"
	EnvAIModel   = "OUTREACH_AI_MODEL"
	EnvSendVia   = "OUTREACH_SEND_VIA"

	// EnvPassword, EnvAIKey and EnvSendAPIKey are the names of environment
	// variables that carry secrets; the values themselves are never persisted.
	EnvPassword   = "OUTREACH_SMTP_PASSWORD" //nolint:gosec // env var name, not a credential
	EnvAIKey      = "OUTREACH_AI_API_KEY"    //nolint:gosec // env var name, not a credential
	EnvSendAPIKey = "OUTREACH_SEND_API_KEY"  //nolint:gosec // env var name, not a credential
)

// ApplyEnvOverrides overlays environment variables on top of stored settings.
func (s *Settings) ApplyEnvOverrides() {
	if v := os.Getenv(EnvSMTPHost); v != "" {
		s.SMTPHost = v
	}

	if v := os.Getenv(EnvSMTPPort); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			s.SMTPPort = p
		}
	}

	if v := os.Getenv(EnvIMAPHost); v != "" {
		s.IMAPHost = v
	}

	if v := os.Getenv(EnvIMAPPort); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			s.IMAPPort = p
		}
	}

	if v := os.Getenv(EnvEmail); v != "" {
		s.EmailAddress = v
	}

	if v := os.Getenv(EnvPassword); v != "" {
		s.Password = v
	}

	if v := os.Getenv(EnvFromName); v != "" {
		s.FromName = v
	}

	if v := os.Getenv(EnvAIBaseURL); v != "" {
		s.AIBaseURL = v
	}

	if v := os.Getenv(EnvAIModel); v != "" {
		s.AIModel = v
	}

	if v := os.Getenv(EnvAIKey); v != "" {
		s.AIAPIKey = v
	}

	if v := os.Getenv(EnvSendVia); v != "" {
		s.SendVia = v
	}

	if v := os.Getenv(EnvSendAPIKey); v != "" {
		s.SendAPIKey = v
	}

	if s.SMTPHost == "" || s.IMAPHost == "" {
		if preset, ok := PresetForEmail(s.EmailAddress); ok {
			if s.SMTPHost == "" {
				s.SMTPHost = preset.SMTPHost
				s.SMTPPort = preset.SMTPPort
				s.SMTPTLS = preset.SMTPTLS
			}

			if s.IMAPHost == "" {
				s.IMAPHost = preset.IMAPHost
				s.IMAPPort = preset.IMAPPort
			}
		}
	}
}

// SMTPConfigured reports whether direct SMTP sending is possible.
func (s *Settings) SMTPConfigured() bool {
	return s.SMTPHost != "" && s.SMTPPort > 0 && s.EmailAddress != "" && s.Password != ""
}

// APISendConfigured reports whether the Brevo API channel is usable.
func (s *Settings) APISendConfigured() bool {
	return s.SendAPIKey != "" && s.EmailAddress != ""
}

// SendConfigured reports whether the currently selected outbound channel is
// ready to send.
func (s *Settings) SendConfigured() bool {
	if s.SendVia == SendViaAPI {
		return s.APISendConfigured()
	}

	return s.SMTPConfigured()
}

// IMAPConfigured reports whether the inbox can be polled.
func (s *Settings) IMAPConfigured() bool {
	return s.IMAPHost != "" && s.IMAPPort > 0 && s.EmailAddress != "" && s.Password != ""
}

// AIConfigured reports whether AI copywriting, reply drafting and intent
// scoring can run.
func (s *Settings) AIConfigured() bool {
	return s.AIBaseURL != "" && s.AIModel != "" && s.AIAPIKey != ""
}

// Provider is a well-known mail platform preset.
type Provider struct {
	Name     string `json:"name"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPTLS  string `json:"smtp_tls"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	// DomainSuffixes trigger auto-detection from the email address.
	DomainSuffixes []string `json:"-"`
}

// Providers lists the built-in presets shown in the settings UI.
func Providers() []Provider {
	return []Provider{
		{
			Name:           "网易免费企业邮箱 (freeqiye.com)",
			SMTPHost:       "smtphz.qiye.163.com",
			SMTPPort:       465,
			SMTPTLS:        TLSModeSSL,
			IMAPHost:       "imaphz.qiye.163.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"freeqiye.com"},
		},
		{
			Name:           "网易企业邮箱 (qiye.163.com)",
			SMTPHost:       "smtp.qiye.163.com",
			SMTPPort:       465,
			SMTPTLS:        TLSModeSSL,
			IMAPHost:       "imap.qiye.163.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"ym.163.com"},
		},
		{
			Name:           "腾讯企业邮箱 (exmail.qq.com)",
			SMTPHost:       "smtp.exmail.qq.com",
			SMTPPort:       465,
			SMTPTLS:        TLSModeSSL,
			IMAPHost:       "imap.exmail.qq.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"exmail.qq.com"},
		},
		{
			Name:           "阿里企业邮箱 (qiye.aliyun.com)",
			SMTPHost:       "smtp.qiye.aliyun.com",
			SMTPPort:       465,
			SMTPTLS:        TLSModeSSL,
			IMAPHost:       "imap.qiye.aliyun.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"mxhichina.com"},
		},
		{
			Name:           "Gmail / Google Workspace",
			SMTPHost:       "smtp.gmail.com",
			SMTPPort:       465,
			SMTPTLS:        TLSModeSSL,
			IMAPHost:       "imap.gmail.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"gmail.com", "googlemail.com"},
		},
		{
			Name:           "Outlook / Microsoft 365",
			SMTPHost:       "smtp.office365.com",
			SMTPPort:       587,
			SMTPTLS:        TLSModeStartTLS,
			IMAPHost:       "outlook.office365.com",
			IMAPPort:       993,
			DomainSuffixes: []string{"outlook.com", "hotmail.com"},
		},
	}
}

// PresetForEmail auto-detects server settings from the email address domain.
// e.g. anything under freeqiye.com maps to the NetEase free enterprise mail
// servers (smtphz.qiye.163.com / imaphz.qiye.163.com).
func PresetForEmail(email string) (Provider, bool) {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return Provider{}, false
	}

	domain := strings.ToLower(email[at+1:])

	for _, p := range Providers() {
		for _, suffix := range p.DomainSuffixes {
			if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
				return p, true
			}
		}
	}

	return Provider{}, false
}
