# ewws-platform-ui

Self-service UI for the MeKo vibe-tenant Kubernetes platform.

Lives at `https://platform.apps.meko.work` (Tailscale-internal, behind Traefik).

## What it does

- **Anonymous**: browse the status of every claimed tenant — slug, owners,
  host, Argo CD sync/health, last deploy. Data comes from
  [`MeKo-Tech/ewws-apps-registry`](https://github.com/MeKo-Tech/ewws-apps-registry)
  (the `apps/*.yaml` files) plus the cluster's Argo CD HTTP API.
- **Logged-in (any MeKo-Tech org member)**: fill the `/claim` form, which
  opens a PR against `ewws-apps-registry` adding `apps/<slug>.yaml`. The PR
  is authored by the **user's own GitHub token** (OAuth2 flow), so audit
  trails stay clean.
- **Admins (`MeKo-Tech:ewws` team)**: get extra `Merge / Suspend` buttons
  in the UI — for MVP these just call the same GitHub PR API as the user.

## Routes

| Route | Auth | Notes |
| --- | --- | --- |
| `GET /` | anonymous | landing + grid of all apps |
| `GET /app/<slug>` | anonymous | detail page (registry YAML + Argo CD status) |
| `GET /claim` | session | claim form |
| `POST /claim` | session + CSRF | opens PR, redirects to it |
| `GET /auth/login` | — | starts GitHub OAuth |
| `GET /auth/callback` | — | finishes GitHub OAuth |
| `POST /auth/logout` | — | clears session |
| `GET /healthz` | — | always 200 |
| `GET /readyz` | — | 200 when OAuth config present |
| `GET /partials/app-status/<slug>` | — | HTMX `<td>` fragment, 30s refresh |
| `GET /static/*` | — | embedded assets |

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

Generate `SESSION_SECRET` like this:

```sh
openssl rand -base64 32
```

## Local dev

```sh
go install github.com/a-h/templ/cmd/templ@v0.3.943
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
