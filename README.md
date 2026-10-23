# ewws-platform-ui

Self-service UI for the MeKo vibe-tenant Kubernetes platform.

Lives at `https://platform.apps.meko.work` (Tailscale-internal, behind Traefik).

## What it does

- **Anonymous**: browse the status of every claimed tenant — slug, owners,
  host, Argo CD sync/health, last deploy. Data comes from
  [`MeKo-Tech/ewws-apps-registry`](https://github.com/MeKo-Tech/ewws-apps-registry)
  (the `apps/*.yaml` files) plus the cluster's Argo CD HTTP API.
- **Logged-in (any MeKo-Tech org member)**: fill the `/claim` form, which
  opens a PR against `ewws-apps-registry` adding `apps/<slug>.yaml` plus
  `tags/<slug>/<stage>.yaml` for every enabled stage (seeded with the
  initial image tag; the app CI overwrites them later), all in one commit.
  The PR is authored by the **user's own GitHub token** (OAuth2 flow), so
  audit trails stay clean. The form validates the claim against what the
  registry's `validate-apps` job checks; the schema copy it is tested
  against lives in `internal/registry/testdata/schema` (the refresh command
  is in `internal/registry/schema_test.go`).
- **Admins (`MeKo-Tech:ewws` team)**: get extra `Merge / Suspend` buttons
  in the UI — for MVP these just call the same GitHub PR API as the user.

## Routes

| Route | Auth | Notes |
| --- | --- | --- |
| `GET /` | anonymous | landing + dashboard of all apps, "Neue App" CTA |
| `GET /app/{slug}` | anonymous | detail page (registry YAML + Argo CD status) |
| `POST /app/{slug}/bootstrap` | session (401 without; no CSRF token) | re-runs the repo bootstrap (test-pr-title, release-please, branch protection) with the user's token; returns an HTMX result fragment |
| `GET /claim` | session (anonymous → redirect to `/auth/login`) | claim form |
| `POST /claim` | session + CSRF | opens the registry PR, bootstraps the repo, renders a success page linking the PR |
| `GET /docs` | anonymous | onboarding + best-practice page |
| `GET /auth/login` | — | starts GitHub OAuth |
| `GET /auth/callback` | — | finishes GitHub OAuth, redirects to `/` |
| `POST /auth/logout` | — | clears session, redirects to `/` |
| `GET /healthz` | — | always 200 |
| `GET /readyz` | — | 200 when OAuth config present, else 503 |
| `GET /partials/app-status/{slug}` | — | HTMX status `<td>` fragment (prod Argo CD state) |
| `GET /partials/compliance/{slug}` | — | HTMX compliance badge fragment |
| `GET /static/` | — | embedded assets |

## Env vars

| Var | Required | Default |
| --- | --- | --- |
| `PORT` | no | `8080` |
| `GITHUB_CLIENT_ID` | yes | — |
| `GITHUB_CLIENT_SECRET` | yes | — |
| `SESSION_SECRET` | yes | — (32-byte base64) |
| `ARGOCD_URL` | no | `http://argo-cd-argocd-server.argocd.svc` |
| `ARGOCD_TOKEN` | yes | read-only Argo CD token |
| `APPS_REGISTRY_REPO` | no | `MeKo-Tech/ewws-apps-registry` |
| `ALLOWED_ORG` | no | `MeKo-Tech` |
| `ADMIN_TEAM` | no | `ewws` |
| `BASE_URL` | no | `https://platform.apps.meko.work` (used for OAuth redirect URI) |
| `LOG_FORMAT` | no | `json` in prod, `text` otherwise |
| `EWWS_AUTH_URL` | no | — (e.g. `https://login.ewws.meko.dev`; enables the read-only scope catalog on `/app/<slug>`) |
| `EWWS_AUTH_CLIENT_ID` | no | — (HTTP Basic client id for `GET /scope-catalog/{name}`) |
| `EWWS_AUTH_CLIENT_SECRET` | no | — (HTTP Basic client secret) |

The three `EWWS_AUTH_*` vars go together: if only some are set, the
integration is disabled with a startup warning (startup never fails on
them). Scope lookups are cached in memory for 5 minutes and time out
after 3 s; the detail page renders regardless of ewws-auth.

Generate `SESSION_SECRET` like this:

```sh
openssl rand -base64 32
```

## Local dev

```sh
go install github.com/a-h/templ/cmd/templ@v0.3.1070
templ generate ./...
go run ./cmd/server
```

Then point your browser at `http://localhost:8080`.

## Regenerating templ output

The `*_templ.go` files are git-ignored — regenerate them before building.

```sh
templ generate ./...
go build ./cmd/server
```

## Distroless image / Helm runAsUser note

The Dockerfile targets `gcr.io/distroless/static-debian12:nonroot`, which
uses UID **65532**. The chart that wraps this image historically pins
`runAsUser: 1000`; override that to `65532` in the chart values, otherwise
the kernel will reject the process. This is documented inline in the
Dockerfile too.
