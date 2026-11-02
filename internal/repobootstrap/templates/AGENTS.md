# Agent instructions — MeKo Vibe-Tenant app

This repo is a Vibe-Tenant app running on the MeKo Platform (chungus K3s
cluster). Full onboarding docs live at
**https://platform.apps.meko.work/docs** — fetch them with `curl` from any
intranet-connected machine; no login is required for `/docs`.
The reference layout is `MeKo-Tech/go-react-template`; copy what is missing
here from there instead of writing your own variant.

## Deployment

- Push to `main` → CI builds the images and writes the `sha-<7>` tag to
  `tags/<slug>/staging.yaml` in `MeKo-Tech/ewws-apps-registry`
  (`write-registry-tag.yml` from `MeKo-Tech/ewws-workflows`).
- Conventional-Commits merge → `release-please` opens a release PR. Merging
  it publishes the release, and CI writes `tags/<slug>/prod.yaml`.
- Image tags live only in `tags/<slug>/<stage>.yaml`; never put a `tag` into
  the claim, never edit tag files by hand.

## Secrets

- Plaintext secrets exist only SOPS-encrypted in `secrets/<stage>.sops.yaml`
  (`staging`, `prod`), encrypted with `sops-key-vibe` in `meko-vibe-vault`.
  `.sops.yaml` routes them there — do not edit it, never use `mekorp-vault*`.
- Tooling: `secrets.just` + `scripts/platform-secrets.sh` from
  go-react-template. `just secrets-login`, then
  `pbpaste | just secrets-set <stage> <KEY>`, then `just secrets-seal`.
- `just secrets-seal` writes `deploy/sealed/<stage>/<slug>-app.yaml`
  (SealedSecret, sealed locally, committed). CI copies it to the registry via
  `write-registry-secrets.yml`; the platform loads `<slug>-app` into the
  backend by itself, no claim entry needed.
- Login: the platform injects `EWWS_AUTH_CLIENT_ID` and `EWWS_AUTH_ISSUER`
  from the claim's `auth.client_ids`. Do not set an issuer or client id by hand.

## Repo conventions (do not delete these files)

| File | What it does |
| --- | --- |
| `.github/workflows/release-please.y(a)ml` | release PRs from Conventional Commits |
| `.github/workflows/test-pr-title.y(a)ml` | Conventional-Commits check on PR titles |
| `release-please-config.json` | release-please tracks Conventional Commits |
| `.release-please-manifest.json` | release-please's current-version log |
| `.sops.yaml` | routes `secrets/*.sops.yaml` to `meko-vibe-vault` — do not edit |
| `AGENTS.md` | this file |

## Forbidden

- No image tag `latest` — Kyverno blocks it on chungus.
- No plaintext credentials in the repo; `.env` files are never committed.
- No secret in the claim, in `values` files or in workflow env.

## When you need more

- `/docs` on the Platform UI — full onboarding guide, FAQ, shared-secrets catalogue.
- `/app/<slug>` on the Platform UI — live status of this tenant
  (Sync, Health, drift between staging and prod, traffic in last 7d/30d,
  the Bootstrap-button to re-run the at-claim setup).
- Slack `#platform` for human help.
