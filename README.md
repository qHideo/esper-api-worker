# esper-api-worker

Cloudflare Worker version of the Esper launcher backend. It serves the same
endpoints as the Go service in `../esper-api`, with static files handled by
Workers Static Assets.

## Endpoints

| Path | Purpose | Cache-Control |
| --- | --- | --- |
| `GET /` | service identity JSON | `no-store` |
| `GET /healthz` | liveness probe, returns `ok` | `no-store` |
| `GET /v1/latest.json` | beta update manifest | `public, max-age=300` |
| `GET /v1/nightly.json` | nightly update manifest | `public, max-age=60` |
| `GET /v1/news.json` | news feed | `public, max-age=60` |
| anything else | files under `public/` | `public, max-age=60` |

Only `GET` and `HEAD` are accepted; missing files and directories are 404.

## Prerequisites

- Node.js 20 or newer (`npx` must be on PATH)
- A free Cloudflare account

## Deployed

- Worker: `esper-api`
- URL: `https://esper-api.esper-api-worker.workers.dev`
- Update any file in `public/`, then rerun `.\deploy.ps1` to publish it.

The `esper-api-worker` workers.dev subdomain can be renamed once in the
dashboard (Workers → Settings → Subdomain); renaming changes the URL, so do it
before wiring the launcher to it.

## Run locally

```powershell
npm install
npx wrangler dev
```

Then check `http://127.0.0.1:8787/healthz` and `/v1/news.json`.

## Deploy

```powershell
npx wrangler login      # one-time browser login
.\deploy.ps1
```

Or `npm run deploy`. The first deploy prints a URL like
`https://esper-api.<your-subdomain>.workers.dev`; that URL is live immediately.

### Custom domain

In the Cloudflare dashboard: Workers & Pages → `esper-api` → Settings →
Domains & Routes → Add → Custom domain, e.g. `api.esper.example`. The domain
must be on Cloudflare (nameservers delegated); Cloudflare issues the TLS
certificate automatically.

Alternatively, add to `wrangler.toml`:

```toml
routes = [
  { pattern = "api.esper.example", custom_domain = true }
]
```

## Free plan

- 100,000 Worker requests/day (resets 00:00 UTC); static asset requests are
  free and not counted toward that limit.
- 10 ms CPU per request, 128 MB memory — far above what this service needs.
- 200,000 log events/day; `wrangler tail` streams them live for debugging.

The free plan never bills: if the daily request cap is exceeded, requests fail
with error 1027 until the next day.

## Automatic releases (GitHub Actions)

Publishing a GitHub release signs its asset and deploys the manifest:

```
GitHub release (tag vX, payload.zip attached) → Actions → sign → deploy Worker
→ launchers update within 10 minutes
```

The workflow is `.github/workflows/release.yml`; the logic is `ci/release.ps1`.
The signer in `release/` is a copy of `esper-api/cmd/release` so the repo is
self-contained — keep them in sync when flags change.

### Setup

1. Push this project to a **public** GitHub repository (release assets must be
   downloadable without authentication). For a private repo, host artifacts on
   R2 instead and pass an explicit URL.
2. Add three repository secrets under Settings → Secrets and variables →
   Actions:

   | Secret | Value |
   | --- | --- |
   | `UPDATE_SIGNING_SEED` | Contents of `esper-api\keys\seed.b64` (one base64 line) |
   | `CLOUDFLARE_API_TOKEN` | Token from the "Edit Cloudflare Workers" template |
   | `CLOUDFLARE_ACCOUNT_ID` | Account ID from the Cloudflare dashboard URL |

3. Commit and push. The workflow must exist on the default branch before
   release events can trigger it.

### Releasing

1. Create a release with a tag such as `v0.1.32`.
2. Attach one `.zip` payload (for example the archive produced by
   `esper-api\release.ps1`).
3. Publish. The workflow downloads the asset, signs the channel manifest,
   verifies it, carries the live news feed and the other channels forward, and
   runs `wrangler deploy`.

Channels map to files the launcher follows:

| Channel | File | Trigger |
| --- | --- | --- |
| `beta` | `public/v1/latest.json` | normal release |
| `nightly` | `public/v1/nightly.json` | release marked **pre-release** |
| `stable` | `public/v1/stable.json` | manual run (reserved) |

You can also run it manually: Actions → **Release manifest** → Run workflow
with the tag and channel.

The build number defaults to the workflow run number, and the manifest expires
after 90 days.

### Local dry run

```powershell
cd C:\Users\Tyler\Downloads\esper-api-worker
.\ci\release.ps1 -Version 0.1.32 `
  -Payload ..\esper-api\dist\placeholder-artifact.bin `
  -AssetUrl https://github.com/owner/repo/releases/download/v0.1.32/Esper-Client.zip `
  -SeedFile ..\esper-api\keys\seed.b64 `
  -Out dist\ci-test.json -NoDeploy
```

`-NoDeploy` signs and verifies without touching Cloudflare; omit `-Payload`
to let `gh` download the release asset, and omit `-AssetUrl` in CI so it is
computed from `GITHUB_REPOSITORY`.

### Notes

- The hash covers the exact asset uploaded; the workflow fails if the release
  has no `.zip` (or warns and uses the first of several).
- The seed must match the public key compiled into the launcher. Rotating it
  means rebuilding the launcher with the new key.
- News edited through the local editor is preserved: the deploy refreshes
  `public/v1/news.json` from the live endpoint first.

## Release manifest

`public/v1/latest.json` is the Ed25519-signed envelope produced by
`../esper-api/cmd/release sign`. Regenerate it for each release, copy it here,
and redeploy:

```powershell
Copy-Item ..\esper-api\public\v1\latest.json public\v1\latest.json -Force
.\deploy.ps1
```

Keep the signing seed in CI or an offline vault — never in Worker code,
variables, or this repository. The Worker only serves opaque bytes. Large
artifacts belong on GitHub Releases or R2; Worker assets are limited to 25 MiB
per file.
