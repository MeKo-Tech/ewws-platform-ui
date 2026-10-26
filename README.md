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
| `GET /usage` | admin session (anonymous → redirect to `/auth/login`, non-admin → 403) | login activity per ewws-auth client_id (1/7/30 days), linked to registry apps via `auth.client_ids` |
| `GET /usage/{client_id}` | admin session (as above) | per-account activity of one client; `?issuer=&from=&to=` (YYYY-MM-DD, at most 366 days); `-` = logins without client_id |
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
| `EWWS_AUTH_URL` | no | — (e.g. `https://login.ewws.meko.dev`; base URL for the scope catalog and `/usage`) |
| `EWWS_AUTH_CLIENT_ID` | no | — (HTTP Basic client id for `GET /scope-catalog/{name}`) |
| `EWWS_AUTH_CLIENT_SECRET` | no | — (HTTP Basic client secret) |
| `EWWS_AUTH_MACHINE_CLIENT_ID` | no | — (Entra app id of the machine account; enables `/usage`) |
| `EWWS_AUTH_MACHINE_CLIENT_SECRET` | no | — (client secret of that Entra app) |

`EWWS_AUTH_URL` is shared by two integrations, each with its own
credential pair: `EWWS_AUTH_CLIENT_ID`/`_SECRET` enable the read-only
scope catalog on `/app/<slug>`, `EWWS_AUTH_MACHINE_CLIENT_ID`/`_SECRET`
enable `/usage`. A pair that is only half set, or set without the URL,
disables just that integration with a startup warning (startup never
fails on them); the URL alone just leaves both off, without a warning.
Scope lookups are cached in memory for 5 minutes and
time out after 3 s; the detail page renders regardless of ewws-auth.

`/usage` signs in to ewws-auth as a machine account: it exchanges the
Entra app credential at `POST /login/azure` (`client_credentials`) and
calls `GET /api/activity/clients[/{client_id}]` with the resulting
Bearer token. The token stays on the server — cached until shortly
before expiry, never sent to the browser, never logged. The machine
account needs an Entra app whose service principal holds the
`machine_role` app role on the ewws-auth Azure app, an App account in
ewws-auth, and the grant `auth:activity:r` (see ewws-auth
`docs/machine-clients.md`). Without the grant the page says so instead
of failing; without the env vars it says "nicht konfiguriert".

Generate `SESSION_SECRET` like this:

```sh
openssl rand -base64 32
```

## Operations

Reviewing and approving a claim PR is more than merging it: the app also
needs its own ewws-auth client, access to the tagger key
(`EWWS_REGISTRY_TAGGER_PRIVATE_KEY`), and an entry in the `vibe-app-repos`
ruleset. The admin runbook (German) covers every step, including withdrawing
an app:
[Neue Vibe-App freischalten](https://github.com/MeKo-Tech/ewws-wiki/blob/main/02_Softwareentwicklung/02_Tech/08_App-Plattform/neue_vibe_app_freischalten.md)
(ewws-wiki).

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
