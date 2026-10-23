package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	ghclient "github.com/MeKo-Tech/ewws-platform-ui/internal/github"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/csrf"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/repobootstrap"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
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

func (h Claim) renderForm(
	w http.ResponseWriter,
	r *http.Request,
	errMsg string,
	prev views.ClaimFormState,
) {
	token, err := csrf.MintToken(w, r)
	if err != nil {
		h.Logger.Error("mint csrf", slog.Any("err", err))
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	user := middleware.UserFromContext(r.Context())

	page := views.PageProps{Title: "Claim", User: userFromCtx(r.Context())}

	render(w, r, views.Claim(page, claimProps(user.Login, errMsg, token, prev)))
}

func claimProps(owner, errMsg, token string, prev views.ClaimFormState) views.ClaimProps {
	return views.ClaimProps{
		DefaultOwner:   owner,
		MemoryOptions:  registry.MemoryOptions,
		CPUOptions:     registry.CPUOptions,
		Error:          errMsg,
		CSRFToken:      token,
		PreviousValues: prev,
	}
}

// parseClaimForm turns the submitted form into a ClaimInput. The returned
// form state always carries the raw input, so the form can be shown again
// with the user's values when parsing or validation fails.
func parseClaimForm(
	form url.Values,
	user *middleware.SessionUser,
) (*registry.ClaimInput, views.ClaimFormState, error) {
	val := func(name string) string { return strings.TrimSpace(form.Get(name)) }
	raw := func(name string) string { return form.Get(name) }
	checked := func(name string) bool { return form.Get(name) != "" }

	component := func(prefix string) views.ClaimComponentFormState {
		return views.ClaimComponentFormState{
			ImageRepo:     val(prefix + "_image_repo"),
			ImageTag:      val(prefix + "_image_tag"),
			MemoryLimit:   val(prefix + "_memory_limit"),
			CPULimit:      val(prefix + "_cpu_limit"),
			Port:          val(prefix + "_port"),
			LivenessPath:  val(prefix + "_liveness_path"),
			ReadinessPath: val(prefix + "_readiness_path"),
			StartupPath:   val(prefix + "_startup_path"),
			Env:           raw(prefix + "_env"),
			EnvFrom:       val(prefix + "_env_from"),
		}
	}

	stage := func(prefix string) views.ClaimStageFormState {
		return views.ClaimStageFormState{
			Host:        val(prefix + "_host"),
			Enabled:     checked(prefix + "_enabled"),
			BackendEnv:  raw(prefix + "_backend_env"),
			FrontendEnv: raw(prefix + "_frontend_env"),
		}
	}

	st := views.ClaimFormState{
		Submitted:     true,
		Slug:          val("slug"),
		RepoURL:       val("repo_url"),
		HasBackend:    checked("has_backend"),
		HasFrontend:   checked("has_frontend"),
		Backend:       component("backend"),
		Frontend:      component("frontend"),
		Staging:       stage("staging"),
		Prod:          stage("prod"),
		AuthScope:     val("auth_scope"),
		Websocket:     checked("websocket"),
		IngressAuth:   val("ingress_auth"),
		Secrets:       raw("secrets"),
		SharedSecrets: val("shared_secrets"),
	}

	in := &registry.ClaimInput{
		Slug:                st.Slug,
		OwnerTechnical:      user.Login,
		OwnerOrganizational: user.Login,
		RepoURL:             st.RepoURL,
		HasBackend:          st.HasBackend,
		HasFrontend:         st.HasFrontend,
		AuthScope:           st.AuthScope,
		Websocket:           st.Websocket,
		IngressAuth:         st.IngressAuth,
		SharedSecrets:       registry.ParseNameList(st.SharedSecrets),
	}

	var err error

	if in.Backend, err = componentInput("backend", st.Backend); err != nil {
		return nil, st, err
	}

	if in.Frontend, err = componentInput("frontend", st.Frontend); err != nil {
		return nil, st, err
	}

	if in.Staging, err = stageInput("staging", st.Staging); err != nil {
		return nil, st, err
	}

	if in.Prod, err = stageInput("prod", st.Prod); err != nil {
		return nil, st, err
	}

	if in.Secrets, err = registry.ParseSecretLines(st.Secrets); err != nil {
		return nil, st, fmt.Errorf("secrets: %w", err)
	}

	return in, st, nil
}

func componentInput(
	name string,
	st views.ClaimComponentFormState,
) (registry.ComponentInput, error) {
	out := registry.ComponentInput{
		ImageRepository: st.ImageRepo,
		ImageTag:        st.ImageTag,
		MemoryLimit:     st.MemoryLimit,
		CPULimit:        st.CPULimit,
		EnvFromSecrets:  registry.ParseNameList(st.EnvFrom),
		LivenessPath:    st.LivenessPath,
		ReadinessPath:   st.ReadinessPath,
		StartupPath:     st.StartupPath,
	}

	if st.Port != "" {
		port, err := strconv.Atoi(st.Port)
		if err != nil {
			return out, fmt.Errorf("%s port %q is not a number", name, st.Port)
		}

		out.Port = port
	}

	env, err := registry.ParseEnvLines(st.Env)
	if err != nil {
		return out, fmt.Errorf("%s env: %w", name, err)
	}

	out.Env = env

	return out, nil
}

func stageInput(name string, st views.ClaimStageFormState) (registry.StageInput, error) {
	out := registry.StageInput{Host: st.Host, Enabled: st.Enabled}

	var err error

	if out.BackendEnv, err = registry.ParseEnvLines(st.BackendEnv); err != nil {
		return out, fmt.Errorf("%s backend env: %w", name, err)
	}

	if out.FrontendEnv, err = registry.ParseEnvLines(st.FrontendEnv); err != nil {
		return out, fmt.Errorf("%s frontend env: %w", name, err)
	}

	return out, nil
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

	input, prev, err := parseClaimForm(r.PostForm, user)
	if err != nil {
		h.renderForm(w, r, err.Error(), prev)
		return
	}

	if err := input.Validate(h.Reserved); err != nil {
		h.renderForm(w, r, err.Error(), prev)
		return
	}

	files, err := claimFiles(input)
	if err != nil {
		h.Logger.Error("render claim files", slog.Any("err", err))
		h.renderForm(w, r, "could not render YAML: "+err.Error(), prev)

		return
	}

	prResult, err := h.openPR(r.Context(), user, input, files)
	if err != nil {
		h.Logger.Error("open PR", slog.Any("err", err))
		h.renderForm(w, r, "could not open PR: "+err.Error(), prev)

		return
	}

	bootstrapResults := h.bootstrapRepo(r.Context(), user, input)

	page := views.PageProps{Title: "Claim opened", User: userFromCtx(r.Context())}

	stagingHost := input.Staging.Host
	if !input.Staging.Enabled {
		stagingHost = input.Prod.Host
	}

	render(w, r, views.ClaimSuccess(page, views.ClaimSuccessData{
		Slug:       input.Slug,
		PRURL:      prResult.URL,
		Actions:    toViewActions(bootstrapResults),
		RepoURL:    input.RepoURL,
		StagingURL: "https://" + stagingHost,
	}))
}

// claimFiles renders apps/<slug>.yaml plus the initial tag files of every
// enabled stage, keyed by their registry path.
func claimFiles(input *registry.ClaimInput) (map[string][]byte, error) {
	yamlBytes, err := input.ToApp().RenderYAML()
	if err != nil {
		return nil, err
	}

	files, err := input.TagFiles()
	if err != nil {
		return nil, err
	}

	files["apps/"+input.Slug+".yaml"] = yamlBytes

	return files, nil
}

// claimPRBody is the description of the registry PR.
func claimPRBody(login string, input *registry.ClaimInput, files map[string][]byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Claim opened via the platform UI by @%s.\n\n", login)
	fmt.Fprintf(&b, "- slug: `%s`\n", input.Slug)
	fmt.Fprintf(&b, "- repo: %s\n", input.RepoURL)

	var comps []string
	if input.HasBackend {
		comps = append(comps, "backend")
	}

	if input.HasFrontend {
		comps = append(comps, "frontend")
	}

	fmt.Fprintf(&b, "- components: %s\n", strings.Join(comps, ", "))

	for _, s := range []struct {
		name string
		in   registry.StageInput
	}{{"staging", input.Staging}, {"prod", input.Prod}} {
		fmt.Fprintf(&b, "- %s: `%s` (enabled: %t)\n", s.name, s.in.Host, s.in.Enabled)
	}

	b.WriteString("\nImages (initial tag for staging and prod):\n\n")

	if input.HasBackend {
		fmt.Fprintf(
			&b,
			"- backend: `%s:%s`\n",
			input.Backend.ImageRepository,
			input.Backend.ImageTag,
		)
	}

	if input.HasFrontend {
		fmt.Fprintf(
			&b,
			"- frontend: `%s:%s`\n",
			input.Frontend.ImageRepository,
			input.Frontend.ImageTag,
		)
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	slices.Sort(paths)

	b.WriteString("\nFiles in this PR:\n\n")

	for _, p := range paths {
		fmt.Fprintf(&b, "- `%s`\n", p)
	}

	b.WriteString(
		"\nThe `tags/" + input.Slug + "/<stage>.yaml` files are seeded with the initial " +
			"tag; the app CI (`write-registry-tag`) overwrites them on its next build.\n",
	)
	b.WriteString("\n`enabled: false` on a stage only takes effect after the platform rollout " +
		"(meko-app chart 1.10.0 and ApplicationSet support); until then the stage is deployed anyway.\n")

	return b.String()
}

// toViewActions adapts the repobootstrap types to the views' simpler shape
// — keeps the templ free of cross-package imports.
func toViewActions(actions []repobootstrap.Action) []views.ClaimAction {
	out := make([]views.ClaimAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, views.ClaimAction{
			Name:    a.Name,
			Status:  string(a.Status),
			Path:    a.Path,
			Message: a.Message,
		})
	}

	return out
}

// bootstrapRepo applies the platform's standard settings to the user's
// target repo (the one referenced from the claim form, NOT the registry
// repo). Uses the user's OAuth token so the audit log shows their login.
// Errors are surfaced per-step on the success page rather than blocking
// the whole flow — the PR is already open at this point, and the
// compliance scanner will pick up any gaps on its next tick anyway.
func (h Claim) bootstrapRepo(
	ctx context.Context,
	user *middleware.SessionUser,
	input *registry.ClaimInput,
) []repobootstrap.Action {
	owner, repo, err := registry.ParseGitHubURL(input.RepoURL)
	if err != nil {
		h.Logger.Warn(
			"skip bootstrap — bad repo URL",
			slog.String("url", input.RepoURL),
			slog.Any("err", err),
		)

		return []repobootstrap.Action{{
			Name:    "Bootstrap target repo",
			Status:  repobootstrap.StatusFailed,
			Message: "could not parse repo URL: " + err.Error(),
		}}
	}

	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: user.Token})
	httpClient := oauth2.NewClient(ctx, src)
	client := github.NewClient(httpClient)

	return repobootstrap.Run(ctx, client, owner, repo, input.Slug)
}

func (h Claim) openPR(
	ctx context.Context,
	user *middleware.SessionUser,
	input *registry.ClaimInput,
	files map[string][]byte,
) (*ghclient.PRResult, error) {
	if user.Token == "" {
		return nil, fmt.Errorf("no OAuth token in session")
	}

	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: user.Token})
	httpClient := oauth2.NewClient(ctx, src)

	req := ghclient.PRRequest{
		Repo:        h.Cfg.AppsRegistryRepo,
		Slug:        input.Slug,
		Files:       files,
		Branch:      "feat/claim-" + input.Slug,
		Title:       "feat: claim " + input.Slug,
		Body:        claimPRBody(user.Login, input, files),
		AuthorLogin: user.Login,
		AuthorEmail: user.Email,
		BaseBranch:  "main",
	}

	return ghclient.OpenClaimPR(ctx, httpClient, req)
}
