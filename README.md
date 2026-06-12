# mljr-data

Data-only repository for [mljr.eu](https://mljr.eu). Scheduled jobs (and
occasional manual edits) update the files here; the homepage checks out this
repo and reads `generated/site-data.json` at runtime, hot-reloading on change.
No API credentials or build steps live in the public web server.

## Files

- `profile.json`: identity, avatar, public links, location, short bio.
- `timeline.json`: work, education, HTL, thesis, and curated milestones.
- `projects.json`: curated projects and manual overrides for GitHub data.
- `generated/strava.json`: public Strava aggregates, no maps or GPS traces.
- `generated/site-data.json`: merged, versioned payload consumed by the homepage.
- `assets/`: checked-in avatars, logos, thesis images, and project screenshots.
- `schemas/site-data.schema.json`: JSON Schema contract for the generated
  homepage artifact.

## Contract

`generated/site-data.json` must validate against
`schemas/site-data.schema.json`.

Required top-level fields:

- `schema_version`: currently `site-data.v1`.
- `generated_at`: RFC3339 timestamp for the generated artifact.
- `github_projects`: merged GitHub/project cards.
- `linkedin_data`: public profile, experience, education, and skills.
- `strava_data`: public aggregate activity data only.

Generators must validate the file before committing it. The homepage parses
the same file with Go `SiteData` types and keeps the previous valid data if a
reload fails.

## Homepage Runtime

The homepage reads `HOMEPAGE_DATA_FILE` at startup and periodically checks its
mtime (`HOMEPAGE_DATA_RELOAD_SECONDS`, default 300). It can point at a checkout
of this repo, so updating `generated/site-data.json` here and re-syncing the
checkout updates the live site with no homepage rebuild required.

## Scheduled Jobs (planned)

- Strava: daily or weekly job using repository secrets for OAuth refresh.
- GitHub: daily job with ETag caching and manual project overrides.
- Merge: deterministic generator that validates schema versions and writes
  `generated/site-data.json`.
