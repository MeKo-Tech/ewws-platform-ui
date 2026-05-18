package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	ghclient "github.com/MeKo-Tech/ewws-platform-ui/internal/github"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/csrf"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Claim handles GET + POST /claim.
type Claim struct {
	Cfg      *config.Config
	Reserved *registry.ReservedSlugs
	Logger   *slog.Logger
}

func (h Claim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.renderForm(w, r, "", views.ClaimFormState{})
	case http.MethodPost:
		h.handleSubmit(w, r, user)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h Claim) renderForm(w http.ResponseWriter, r *http.Request, errMsg string, prev views.ClaimFormState) {
	token, err := csrf.MintToken(w, r)
	if err != nil {
		h.Logger.Error("mint csrf", slog.Any("err", err))
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	user := middleware.UserFromContext(r.Context())

	page := views.PageProps{Title: "Claim", User: userFromCtx(r.Context())}

	props := views.ClaimProps{
		DefaultOwner:   user.Login,
		MemoryOptions:  registry.MemoryOptions,
		CPUOptions:     registry.CPUOptions,
		Error:          errMsg,
		CSRFToken:      token,
		PreviousValues: prev,
	}

	render(w, r, views.Claim(page, props))
}

func (h Claim) handleSubmit(w http.ResponseWriter, r *http.Request, user *middleware.SessionUser) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	if err := csrf.Verify(r); err != nil {
		h.Logger.Warn("csrf failed", slog.Any("err", err))
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	prev := views.ClaimFormState{
		Slug:                r.FormValue("slug"),
		RepoURL:             r.FormValue("repo_url"),
		HasBackend:          r.FormValue("has_backend") != "",
		HasFrontend:         r.FormValue("has_frontend") != "",
		BackendImageRepo:    r.FormValue("backend_image_repo"),
		BackendImageTag:     r.FormValue("backend_image_tag"),
		FrontendImageRepo:   r.FormValue("frontend_image_repo"),
		FrontendImageTag:    r.FormValue("frontend_image_tag"),
		BackendMemoryLimit:  r.FormValue("backend_memory_limit"),
		BackendCPULimit:     r.FormValue("backend_cpu_limit"),
		FrontendMemoryLimit: r.FormValue("frontend_memory_limit"),
		FrontendCPULimit:    r.FormValue("frontend_cpu_limit"),
		StagingHost:         r.FormValue("staging_host"),
		ProdHost:            r.FormValue("prod_host"),
	}

	input := &registry.ClaimInput{
		Slug:                prev.Slug,
		OwnerTechnical:      user.Login,
		OwnerOrganizational: user.Login,
		RepoURL:             prev.RepoURL,
		HasBackend:          prev.HasBackend,
		HasFrontend:         prev.HasFrontend,
		BackendMemoryLimit:  prev.BackendMemoryLimit,
		BackendCPULimit:     prev.BackendCPULimit,
		FrontendMemoryLimit: prev.FrontendMemoryLimit,
		FrontendCPULimit:    prev.FrontendCPULimit,
		StagingHost:         prev.StagingHost,
		ProdHost:            prev.ProdHost,
		BackendImageRepo:    prev.BackendImageRepo,
		BackendImageTag:     prev.BackendImageTag,
		FrontendImageRepo:   prev.FrontendImageRepo,
		FrontendImageTag:    prev.FrontendImageTag,
	}

	if err := input.Validate(h.Reserved); err != nil {
		h.renderForm(w, r, err.Error(), prev)
		return
	}

	app := input.ToApp()

	yamlBytes, err := app.RenderYAML()
	if err != nil {
		h.Logger.Error("render yaml", slog.Any("err", err))
		h.renderForm(w, r, "could not render YAML: "+err.Error(), prev)

		return
	}

	prResult, err := h.openPR(r.Context(), user, input, yamlBytes)
	if err != nil {
		h.Logger.Error("open PR", slog.Any("err", err))
		h.renderForm(w, r, "could not open PR: "+err.Error(), prev)

		return
	}

	http.Redirect(w, r, prResult.URL, http.StatusSeeOther)
}

func (h Claim) openPR(ctx context.Context, user *middleware.SessionUser, input *registry.ClaimInput, yamlBytes []byte) (*ghclient.PRResult, error) {
	if user.Token == "" {
		return nil, fmt.Errorf("no OAuth token in session")
	}

	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: user.Token})
	httpClient := oauth2.NewClient(ctx, src)

	body := fmt.Sprintf(
		"Claim opened via the platform UI by @%s.\n\n"+
			"- slug: `%s`\n"+
			"- repo: %s\n"+
			"- staging host: %s\n"+
			"- prod host: %s\n"+
			"- backend image: `%s:%s`\n"+
			"- frontend image: `%s:%s`\n\n"+
			"Resources: backend `%s` / `%s` CPU, frontend `%s` / `%s` CPU.",
		user.Login,
		input.Slug,
		input.RepoURL,
		input.StagingHost,
		input.ProdHost,
		input.BackendImageRepo, input.BackendImageTag,
		input.FrontendImageRepo, input.FrontendImageTag,
		input.BackendMemoryLimit, input.BackendCPULimit,
		input.FrontendMemoryLimit, input.FrontendCPULimit,
	)

	req := ghclient.PRRequest{
		Repo:        h.Cfg.AppsRegistryRepo,
		Slug:        input.Slug,
		YAMLContent: yamlBytes,
		Branch:      "feat/claim-" + input.Slug,
		Title:       "feat: claim " + input.Slug,
		Body:        body,
		AuthorLogin: user.Login,
		AuthorEmail: user.Email,
		BaseBranch:  "main",
	}

	return ghclient.OpenClaimPR(ctx, httpClient, req)
}
