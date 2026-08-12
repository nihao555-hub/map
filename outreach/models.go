// Package outreach implements a cold-email outreach module: it imports leads
// scraped from Google Maps, sends personalized multi-step email sequences via
// SMTP at reply-optimized times, and polls an IMAP inbox to detect replies,
// bounces and unsubscribe requests.
package outreach

import (
	"errors"
	"time"
)

// Campaign statuses.
const (
	CampaignStatusDraft  = "draft"
	CampaignStatusActive = "active"
	CampaignStatusPaused = "paused"
	CampaignStatusDone   = "done"
)

// Contact statuses.
const (
	// ContactStatusActive means the contact is in the sequence and more
	// steps are scheduled.
	ContactStatusActive = "active"
	// ContactStatusReplied means the contact answered; the sequence stops.
	ContactStatusReplied = "replied"
	// ContactStatusBounced means delivery failed permanently.
	ContactStatusBounced = "bounced"
	// ContactStatusUnsubscribed means the contact asked to stop.
	ContactStatusUnsubscribed = "unsubscribed"
	// ContactStatusCompleted means all steps were sent without a reply.
	ContactStatusCompleted = "completed"
	// ContactStatusFailed means sending failed repeatedly.
	ContactStatusFailed = "failed"
)

// Message directions.
const (
	DirectionOut = "out"
	DirectionIn  = "in"
)

// Inbound message kinds.
const (
	InboundKindReply       = "reply"
	InboundKindBounce      = "bounce"
	InboundKindUnsubscribe = "unsubscribe"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("outreach: not found")

// SequenceStep is a single email in a campaign sequence.
// The first step opens a new thread. Follow-up steps are sent as replies in
// the same thread ("Re: <original subject>") because follow-ups in-thread get
// substantially more answers than fresh threads.
type SequenceStep struct {
	// Subject is a text/template string. Only used for the first step;
	// follow-ups reuse the thread subject.
	Subject string `json:"subject"`
	// Body is a text/template string (plain text).
	Body string `json:"body"`
	// DelayDays is the number of days to wait after the previous step.
	// Ignored for the first step.
	DelayDays int `json:"delay_days"`
}

// Campaign groups contacts and the sequence sent to them.
type Campaign struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	JobID  string `json:"job_id"`
	// ValueProposition must describe the recipient's outcome, not a list of
	// sender features. It is rendered into every step.
	ValueProposition string `json:"value_proposition"`
	// Proof is optional verifiable evidence (customer result, case study or
	// concrete credential). It must never be fabricated.
	Proof string `json:"proof"`
	// CallToAction is one low-friction question used in the opening email.
	CallToAction string         `json:"call_to_action"`
	Sequence     []SequenceStep `json:"sequence"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Validate checks that the campaign can be persisted.
func (c *Campaign) Validate() error {
	if c.ID == "" {
		return errors.New("missing id")
	}

	if c.Name == "" {
		return errors.New("missing name")
	}

	if c.ValueProposition == "" {
		return errors.New("missing value proposition")
	}

	if len(c.Sequence) == 0 {
		return errors.New("missing sequence steps")
	}

	for i := range c.Sequence {
		if c.Sequence[i].Body == "" {
			return errors.New("sequence step with empty body")
		}

		if i == 0 && c.Sequence[i].Subject == "" {
			return errors.New("first sequence step needs a subject")
		}
	}

	return nil
}

// Contact is a single lead inside a campaign.
type Contact struct {
	ID         int64  `json:"id"`
	CampaignID string `json:"campaign_id"`
	Email      string `json:"email"`
	// Name is the business name (Google Maps title).
	Name     string `json:"name"`
	Category string `json:"category"`
	Address  string `json:"address"`
	City     string `json:"city"`
	Website  string `json:"website"`
	Phone    string `json:"phone"`
	Rating   string `json:"rating"`
	// ReviewCount is kept with Rating so the opener can mention credible
	// social proof instead of using a generic compliment.
	ReviewCount int `json:"review_count"`
	// Timezone is an IANA zone like "Europe/Berlin"; used to send during
	// the recipient's local morning.
	Timezone string `json:"timezone"`
	Status   string `json:"status"`
	// NextStep is the index of the next sequence step to send.
	NextStep int `json:"next_step"`
	// NextSendAt is when the next step may be sent (UTC).
	NextSendAt time.Time `json:"next_send_at"`
	LastSentAt time.Time `json:"last_sent_at"`
	// RootMessageID/RootSubject identify the thread opened by step one.
	RootMessageID string    `json:"root_message_id"`
	RootSubject   string    `json:"root_subject"`
	LastMessageID string    `json:"last_message_id"`
	SendFailures  int       `json:"send_failures"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Message is a sent or received email linked to a contact.
type Message struct {
	ID         int64     `json:"id"`
	CampaignID string    `json:"campaign_id"`
	ContactID  int64     `json:"contact_id"`
	Direction  string    `json:"direction"`
	Kind       string    `json:"kind"`
	Step       int       `json:"step"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	MessageID  string    `json:"message_id"`
	InReplyTo  string    `json:"in_reply_to"`
	FromEmail  string    `json:"from_email"`
	ToEmail    string    `json:"to_email"`
	CreatedAt  time.Time `json:"created_at"`
}

// CampaignStats aggregates per-campaign counters for the UI.
type CampaignStats struct {
	Contacts     int `json:"contacts"`
	Sent         int `json:"sent"`
	Replied      int `json:"replied"`
	Bounced      int `json:"bounced"`
	Unsubscribed int `json:"unsubscribed"`
	Completed    int `json:"completed"`
}

// ReplyRate returns replied/contacted as a percentage string helper value.
func (s *CampaignStats) ReplyRate() float64 {
	contacted := s.Sent
	if contacted == 0 {
		return 0
	}

	return float64(s.Replied) / float64(contacted) * 100
}
