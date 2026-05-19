package handlers

import (
	"fmt"
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/compliance"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
)

// PartialCompliance renders the compliance badge for one slug. HTMX swap
// target on the landing-page row.
type PartialCompliance struct {
	Cfg   *config.Config
	Store *compliance.Store
}

func (h PartialCompliance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	if !registry.SlugRegex.MatchString(slug) {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}

	if h.Store == nil {
		writeBadge(w, "—", "unknown", "compliance-disabled")
		return
	}

	results, err := h.Store.ListBySlug(r.Context(), slug)
	if err != nil {
		writeBadge(w, "err", "fail", err.Error())
		return
	}

	if len(results) == 0 {
		writeBadge(w, "—", "unknown", "no scan yet")
		return
	}

	pass, fail, errs := 0, 0, 0
	tooltip := ""
	for _, res := range results {
		switch res.Status {
		case compliance.StatusPass:
			pass++
		case compliance.StatusFail:
			fail++
			if tooltip != "" {
				tooltip += "; "
			}
			tooltip += res.CheckName + ": " + res.Details
		case compliance.StatusError:
			errs++
		}
	}

	label := fmt.Sprintf("%d/%d", pass, pass+fail+errs)
	tone := "ok"
	switch {
	case fail > 0:
		tone = "fail"
	case errs > 0 && pass == 0:
		tone = "unknown"
	}

	if tooltip == "" {
		tooltip = fmt.Sprintf("%d passing", pass)
	}

	writeBadge(w, label, tone, tooltip)
}

// writeBadge emits the same <td><span class="badge badge--<tone>"> shape
// the status-cell partial uses, so the HTMX swap drops in cleanly.
func writeBadge(w http.ResponseWriter, label, tone, tooltip string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w,
		`<td class="status-cell" data-status=%q><span class="badge badge--%s" title=%q>%s</span></td>`,
		tone, tone, tooltip, label,
	)
}
