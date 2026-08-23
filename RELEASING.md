# Release process

Agent Root Broker releases are built only by the tagged GitHub Actions workflow. The workflow runs
the required CI suite, builds reproducible Linux archives and Debian packages, creates SBOMs and
checksums, attests the artifacts, publishes the GitHub Release, and then uploads the same `.deb`
files to Cloudsmith.

Do not build or upload a public release from a maintainer workstation.

## Cloudsmith bootstrap

Cloudsmith Open Source repositories must be created in the web application rather than through the
API. Create one public Open Source repository, then configure:

1. A service account such as `github-actions-publisher` with only the repository permissions needed
   to upload packages. Do not use a maintainer's personal API key in GitHub.
2. An OpenID Connect provider with URL `https://token.actions.githubusercontent.com`, mapped only to
   that service account.
3. These required token claims:

   - `aud`: `https://github.com/Chang-LL`
   - `repository_owner`: `Chang-LL`
   - `repository`: `Chang-LL/agent-root-broker`
   - `ref`: `refs/tags/v.*`
   - `environment`: `cloudsmith-publish`

   Cloudsmith accepts `.*` only as a suffix wildcard. The exact repository, tag prefix, audience,
   and GitHub environment prevent unrelated GitHub workflows from authenticating as the publisher.
4. A GitHub environment named `cloudsmith-publish`, restricted to tags matching `v*`, with these
   environment variables:

   - `CLOUDSMITH_NAMESPACE`: Cloudsmith workspace slug
   - `CLOUDSMITH_REPOSITORY`: Cloudsmith repository slug
   - `CLOUDSMITH_SERVICE_SLUG`: publisher service-account slug

The release workflow grants `id-token: write` only to the Cloudsmith publishing job. The pinned
Cloudsmith action exchanges the GitHub identity for a short-lived credential; no long-lived
Cloudsmith credential is stored in GitHub.

Cloudsmith tracks Open Source repository quota separately from the workspace plan, but its public
Open Source policy does not promise that service accounts and OIDC remain enabled after a premium
trial. Confirm the current entitlement before the trial expires and after any plan change. The
release must fail closed if OIDC exchange is unavailable; do not silently replace it with a
maintainer's long-lived API token.

## Rehearse and publish

Before tagging, run the Release workflow manually with a unique dry-run version such as
`v0.1.0-alpha.5-dry-run`. Download its `rootbroker-release-*` artifact and inspect the checksums,
package names, versions, maintainer scripts, and archive contents. A dry run never publishes to
GitHub Releases or Cloudsmith.

After the release PR and dry run pass:

1. Create the signed or annotated version tag from the reviewed `main` commit and push it.
2. Confirm every reused CI job passes in the tag workflow.
3. Confirm the GitHub Release contains both architectures, both Debian packages, the Homebrew
   formula, checksums, SBOMs inside the archives, and build-provenance attestations.
   Run `brew style agent-root-broker.rb` against the downloaded formula.
4. Confirm Cloudsmith receives both Debian packages. Prerelease tags publish to the `alpha`
   component; stable tags publish to `main`.
5. Test repository setup, `apt install agent-root-broker`, explicit `rootbroker-setup`, an upgrade,
   `rootbroker-uninstall`, and package removal on clean supported hosts.
6. Open a pull request that replaces `Formula/agent-root-broker.rb` in `Chang-LL/homebrew-tap` with
   the exact release asset; require the tap's Linuxbrew install and formula tests before merging.

Never replace an already published package version. If publishing fails before Cloudsmith receives
the package, fix the configuration and rerun only the failed job. If any artifact itself is wrong,
publish a new version.
