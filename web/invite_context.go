package web

import "context"

type inviteCtxKey struct{}

// WithInviteCode stores the redeemed invite code on the request context.
func WithInviteCode(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, inviteCtxKey{}, code)
}

// InviteCodeFromContext returns the invite-code tenant for this request, if any.
func InviteCodeFromContext(ctx context.Context) string {
	v, _ := ctx.Value(inviteCtxKey{}).(string)
	return v
}
