package outreach

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// CampaignView combines a campaign with counters for UI/API responses.
type CampaignView struct {
	Campaign
	Stats CampaignStats `json:"stats"`
}

// SettingsView exposes configuration state without exposing the authorization
// code.
type SettingsView struct {
	Settings
	PasswordConfigured bool `json:"password_configured"`
	SMTPConfigured     bool `json:"smtp_configured"`
	IMAPConfigured     bool `json:"imap_configured"`
}

// CampaignInput is the operator-authored strategy for a new campaign.
type CampaignInput struct {
	Name             string `json:"name"`
	JobID            string `json:"job_id"`
	ValueProposition string `json:"value_proposition"`
	Proof            string `json:"proof"`
	CallToAction     string `json:"call_to_action"`
	Start            bool   `json:"start"`
}

// Service is the application boundary used by the web server.
type Service struct {
	store      *Store
	engine     *Engine
	dataFolder string
}

// NewService creates an outreach application service.
func NewService(store *Store, engine *Engine, dataFolder string) *Service {
	return &Service{
		store:      store,
		engine:     engine,
		dataFolder: dataFolder,
	}
}

// CreateCampaignFromJob creates a draft campaign and imports recipients from
// an existing Google Maps web job CSV.
func (s *Service) CreateCampaignFromJob(
	ctx context.Context,
	input CampaignInput,
) (CampaignView, ImportSummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return CampaignView{}, ImportSummary{}, errors.New("campaign name is required")
	}

	valueProposition := strings.TrimSpace(input.ValueProposition)
	if valueProposition == "" {
		return CampaignView{}, ImportSummary{}, errors.New("value proposition is required")
	}

	jobID := strings.TrimSpace(input.JobID)
	if _, err := uuid.Parse(jobID); err != nil {
		return CampaignView{}, ImportSummary{}, errors.New("invalid map job ID")
	}

	callToAction := strings.TrimSpace(input.CallToAction)
	if callToAction == "" {
		callToAction = "Would a quick 10-minute conversation be useful?"
	}

	campaign := Campaign{
		ID:               uuid.NewString(),
		Name:             name,
		Status:           CampaignStatusDraft,
		JobID:            jobID,
		ValueProposition: valueProposition,
		Proof:            strings.TrimSpace(input.Proof),
		CallToAction:     callToAction,
		Sequence:         DefaultSequence(),
	}

	if err := s.store.CreateCampaign(ctx, &campaign); err != nil {
		return CampaignView{}, ImportSummary{}, err
	}

	csvPath := filepath.Join(s.dataFolder, jobID+".csv")
	file, err := os.Open(csvPath)
	if err != nil {
		_ = s.store.DeleteCampaign(ctx, campaign.ID)

		return CampaignView{}, ImportSummary{}, fmt.Errorf("open map job results: %w", err)
	}
	defer file.Close()

	summary, err := ImportCSV(ctx, s.store, campaign.ID, file, s.engine.now().UTC())
	if err != nil {
		_ = s.store.DeleteCampaign(ctx, campaign.ID)

		return CampaignView{}, ImportSummary{}, err
	}

	if summary.Inserted == 0 {
		_ = s.store.DeleteCampaign(ctx, campaign.ID)

		return CampaignView{}, summary, errors.New("no valid email addresses found in this map job")
	}

	stats, err := s.store.Stats(ctx, campaign.ID)
	if err != nil {
		return CampaignView{}, summary, err
	}

	return CampaignView{Campaign: campaign, Stats: stats}, summary, nil
}

// Campaigns returns all campaigns with aggregate counters.
func (s *Service) Campaigns(ctx context.Context) ([]CampaignView, error) {
	campaigns, err := s.store.Campaigns(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]CampaignView, 0, len(campaigns))

	for i := range campaigns {
		stats, err := s.store.Stats(ctx, campaigns[i].ID)
		if err != nil {
			return nil, err
		}

		views = append(views, CampaignView{Campaign: campaigns[i], Stats: stats})
	}

	return views, nil
}

// Campaign returns one campaign and its counters.
func (s *Service) Campaign(ctx context.Context, id string) (CampaignView, error) {
	campaign, err := s.store.Campaign(ctx, id)
	if err != nil {
		return CampaignView{}, err
	}

	stats, err := s.store.Stats(ctx, id)
	if err != nil {
		return CampaignView{}, err
	}

	return CampaignView{Campaign: campaign, Stats: stats}, nil
}

// Contacts lists campaign contacts.
func (s *Service) Contacts(ctx context.Context, campaignID string, limit int) ([]Contact, error) {
	return s.store.Contacts(ctx, campaignID, limit)
}

// Messages lists inbox/sent messages.
func (s *Service) Messages(ctx context.Context, campaignID string, limit int) ([]Message, error) {
	return s.store.Messages(ctx, campaignID, limit)
}

// SetCampaignStatus starts or pauses a campaign.
func (s *Service) SetCampaignStatus(ctx context.Context, id, status string) error {
	if status == CampaignStatusActive {
		stats, err := s.store.Stats(ctx, id)
		if err != nil {
			return err
		}

		if stats.Contacts == 0 {
			return errors.New("cannot start a campaign without contacts")
		}
	}

	return s.store.SetCampaignStatus(ctx, id, status)
}

// DeleteCampaign deletes a campaign.
func (s *Service) DeleteCampaign(ctx context.Context, id string) error {
	return s.store.DeleteCampaign(ctx, id)
}

// Settings returns a redacted view of current settings.
func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return SettingsView{}, err
	}

	view := SettingsView{
		Settings:           settings,
		PasswordConfigured: settings.Password != "",
		SMTPConfigured:     settings.SMTPConfigured(),
		IMAPConfigured:     settings.IMAPConfigured(),
	}
	view.Password = ""

	return view, nil
}

// SaveSettings validates and stores settings. A non-empty authorization code
// is held in memory only; use OUTREACH_SMTP_PASSWORD for restart persistence.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	if settings.EmailAddress == "" {
		return errors.New("email address is required")
	}

	if _, valid := validRecipient(settings.EmailAddress); !valid {
		return errors.New("invalid email address")
	}

	if settings.SMTPPort < 1 || settings.SMTPPort > 65535 ||
		settings.IMAPPort < 1 || settings.IMAPPort > 65535 {
		return errors.New("mail server port must be between 1 and 65535")
	}

	if settings.SendStartHour < 0 || settings.SendStartHour > 23 ||
		settings.SendEndHour < 1 || settings.SendEndHour > 24 ||
		settings.SendEndHour <= settings.SendStartHour {
		return errors.New("invalid sending hours")
	}

	if settings.DailyCap < 1 || settings.DailyCap > 500 {
		return errors.New("daily cap must be between 1 and 500")
	}

	return s.store.SaveSettings(ctx, settings)
}

// TestConnections verifies SMTP/IMAP without sending email.
func (s *Service) TestConnections(ctx context.Context) error {
	return s.engine.TestConnections(ctx)
}

// Tick runs one engine cycle.
func (s *Service) Tick(ctx context.Context) (TickReport, error) {
	return s.engine.Tick(ctx)
}

// Reply sends a human-approved threaded reply.
func (s *Service) Reply(ctx context.Context, contactID int64, body string) (Message, error) {
	return s.engine.Reply(ctx, contactID, body)
}
