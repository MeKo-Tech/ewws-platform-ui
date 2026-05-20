# Agent instructions — MeKo Vibe-Tenant app

This repo is a Vibe-Tenant app running on the MeKo Platform (chungus K3s
cluster). Full onboarding docs live at
**https://platform.apps.meko.work/docs** — fetch them with `curl` from any
intranet-connected machine; no login is required for `/docs`.

## Deployment in two lines

- Push to `main` → live on `https://<slug>.apps.meko.dev` (staging) within ~3 min.
- Conventional-Commits merge → `release-please` cuts a `vX.Y.Z` tag PR.
  Merge that PR → live on `https://<slug>.apps.meko.work` (prod) within ~3 min.

argocd-image-updater bumps the registry's image-tag fields automatically;
you never edit them by hand after the first claim.

## Repo conventions (do not delete these files)

| File | What it does |
| --- | --- |
| `.github/workflows/docker.yml` | thin wrapper around `MeKo-Tech/ewws-workflows/.github/workflows/build-and-push.yml@v1` |
| `.github/workflows/release-please.yml` | thin wrapper around `…/release-please.yml@v1` |
| `.github/workflows/test-pr-title.yml` | thin wrapper around `…/test-pr-title.yml@v1` |
| `release-please-config.json` | release-please tracks Conventional Commits |
| `.release-please-manifest.json` | release-please's current-version log |
| `version.txt` | release-please bumps this |
| `.sops.yaml` | pins encryption to MeKo Azure Key Vault — do not edit |
| `values.env` | plain non-secret config; one `KEY=value` per line |
| `values.sops.env` | secrets, sops-encrypted; edit via `sops values.sops.env` |
| `AGENTS.md` | this file |

Per-stage overlays are optional: `values.staging.env`, `values.prod.env`,
`values.staging.sops.env`, `values.prod.sops.env`. The chart merges base +
stage at deploy time.

## Forbidden

- No image tag `latest` — Kyverno blocks it on chungus.
- No custom Docker-build or release workflows; always use the `@v1` reusables.
- No plaintext credentials anywhere except inside `values.sops.env` *before*
  the next encrypt-in-place.

## When you need more

- `/docs` on the Platform UI — full onboarding guide, FAQ, shared-secrets catalogue.
- `/app/<slug>` on the Platform UI — live status of this tenant
  (Sync, Health, drift between staging and prod, traffic in last 7d/30d,
  the Bootstrap-button to re-run the at-claim setup).
- Slack `#platform` for human help.
