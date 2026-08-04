package web

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// InviteCookieName is the HttpOnly session cookie set after redeeming an invite code.
	InviteCookieName = "gms_invite_session"
	// InviteSessionTTL is how long a redeemed session stays valid.
	InviteSessionTTL = 30 * 24 * time.Hour
	// DefaultInviteSeedCount is how many codes to generate on first boot when required.
	DefaultInviteSeedCount = 100
)

// ErrInvalidInvite is returned when a code is missing, already used, or malformed.
var ErrInvalidInvite = errors.New("invalid or already used invite code")

// InviteStore persists invite codes and browser sessions.
type InviteStore interface {
	EnsureSeed(ctx context.Context, count int) (created []string, err error)
	Redeem(ctx context.Context, code string) (sessionToken string, expiresAt time.Time, err error)
	ValidSession(ctx context.Context, token string) (bool, error)
	// SessionInviteCode returns the invite code bound to a valid session token.
	SessionInviteCode(ctx context.Context, token string) (string, error)
	Stats(ctx context.Context) (total, used, unused int, err error)
	ListAll(ctx context.Context) ([]InviteCode, error)
	ExportFile(ctx context.Context, path string) error
}

// InviteCode is a single invite entry for listing/export.
type InviteCode struct {
	Code      string
	CreatedAt time.Time
	UsedAt    *time.Time
	SessionID string
}

// InviteRequired reports whether the invite gate is enabled.
// Default: enabled (INVITE_REQUIRED unset or "1"/"true"/"yes").
// Set INVITE_REQUIRED=0 to disable locally.
func InviteRequired() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("INVITE_REQUIRED")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// InviteSeedCount returns how many codes to seed (default 100).
func InviteSeedCount() int {
	v := strings.TrimSpace(os.Getenv("INVITE_SEED_COUNT"))
	if v == "" {
		return DefaultInviteSeedCount
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return DefaultInviteSeedCount
	}
	if n > 10000 {
		return 10000
	}
	return n
}

// GenerateInviteCode creates a code like GMS-XXXX-XXXX (no ambiguous chars).
func GenerateInviteCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	part := func() (string, error) {
		b := make([]byte, 4)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			b[i] = alphabet[n.Int64()]
		}
		return string(b), nil
	}
	a, err := part()
	if err != nil {
		return "", err
	}
	b, err := part()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("GMS-%s-%s", a, b), nil
}

// NormalizeInviteCode uppercases and strips spaces/dashes variants for lookup.
func NormalizeInviteCode(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "")
	// Accept GMSXXXXXXXX or GMS-XXXX-XXXX
	if strings.HasPrefix(s, "GMS") && !strings.Contains(s, "-") && len(s) == 11 {
		return fmt.Sprintf("GMS-%s-%s", s[3:7], s[7:11])
	}
	return s
}

// SeedAndExport ensures invite codes exist and writes the full list to path when non-empty.
func SeedAndExport(ctx context.Context, store InviteStore, count int, exportPath string) error {
	if store == nil {
		return fmt.Errorf("invite store is nil")
	}
	created, err := store.EnsureSeed(ctx, count)
	if err != nil {
		return err
	}
	if len(created) > 0 {
		fmt.Fprintf(os.Stderr, "invite: seeded %d new codes (target total %d)\n", len(created), count)
	}
	if exportPath == "" {
		return nil
	}
	if err := store.ExportFile(ctx, exportPath); err != nil {
		return fmt.Errorf("export invite codes: %w", err)
	}
	total, used, unused, err := store.Stats(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "invite: codes total=%d used=%d unused=%d file=%s\n", total, used, unused, exportPath)
	return nil
}
