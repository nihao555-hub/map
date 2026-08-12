package outreach_test

import (
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestClassifyInboundDoesNotTreatQuotedFooterAsUnsubscribe(t *testing.T) {
	t.Parallel()

	message := outreach.InboundEmail{
		FromEmail: "owner@example.com",
		Subject:   "Re: Quick question",
		Body: `Yes, I'd like to hear more. Can you send pricing?

On Tue, Alex wrote:
> If you'd rather not hear from me again, reply unsubscribe.`,
	}

	if got := outreach.ClassifyInbound(&message); got != outreach.InboundKindReply {
		t.Fatalf("ClassifyInbound() = %q, want reply", got)
	}
}

func TestClassifyInboundRecognizesOptOutAndBounce(t *testing.T) {
	t.Parallel()

	optOut := outreach.InboundEmail{Body: "Please remove me from your list."}
	if got := outreach.ClassifyInbound(&optOut); got != outreach.InboundKindUnsubscribe {
		t.Fatalf("opt-out classified as %q", got)
	}

	bounce := outreach.InboundEmail{
		FromEmail: "mailer-daemon@example.com",
		Subject:   "Delivery Status Notification (Failure)",
	}
	if got := outreach.ClassifyInbound(&bounce); got != outreach.InboundKindBounce {
		t.Fatalf("bounce classified as %q", got)
	}
}
