package views

import (
	"strings"
	"sync"
	"time"
)

// berlin is Europe/Berlin, loaded once; UTC when the tzdata is missing.
var berlin = sync.OnceValue(func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.UTC
	}

	return loc
})

// fmtUsageTime renders a last-seen timestamp as "02.01.2006 15:04" in
// Europe/Berlin; "—" for the zero time.
func fmtUsageTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}

	return t.In(berlin()).Format("02.01.2006 15:04")
}

// usageClientLabel renders a client_id cell; "" is the bucket of logins
// that named no client.
func usageClientLabel(clientID string) string {
	if clientID == "" {
		return "(ohne client_id)"
	}

	return clientID
}

// usageIssuerLabel renders the issuer filter; "" means all issuers.
func usageIssuerLabel(issuer string) string {
	if issuer == "" {
		return "alle"
	}

	return issuer
}

// usageDetailTitle is the detail heading: the ewws-auth name, else the
// client_id label.
func usageDetailTitle(d UsageDetail) string {
	if d.Name != "" {
		return d.Name
	}

	return usageClientLabel(d.ClientID)
}

func usageStages(stages []string) string {
	return strings.Join(stages, ", ")
}
