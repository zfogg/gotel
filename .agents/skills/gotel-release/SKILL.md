---
name: gotel-release
description: Release gotel's C, Go, Python, and TypeScript packages and update projects that consume gotel. Use for versioned GitHub releases or coordinated gotel dependency rollouts.
---

# Release gotel

The version is shared by `c/CMakeLists.txt`, `python/pyproject.toml`, `package.json`, and the Git tag. The release workflow is in [`.github/workflows/release.yml`](../../../.github/workflows/release.yml); its packaging details are in [`scripts/package_release.py`](../../../scripts/package_release.py). Read those files when changing the release format.

Before tagging, check the current branch, working tree, existing tags and releases. Bump the three package versions together and refresh `python/uv.lock` with uv, run relevant local checks, commit and push. Run the workflow manually with `gh workflow run release.yml -f tag=vX.Y.Z`; if API authentication is unavailable but Git push works, push the candidate commit to `codex/release-preflight` instead. Either preflight builds and tests C static and shared libraries on Linux, macOS, and Windows, checks Go, Python, and TypeScript, and uploads temporary Actions artifacts without publishing a release. Require every job to pass before pushing an annotated `vX.Y.Z` tag. The tag-triggered workflow publishes the GitHub Release. Verify the release assets and `SHA256SUMS`, and confirm the latest-release link resolves to the new version.

The 12 C ZIPs contain x64 and ARM64 static and shared builds for Linux, macOS, and Windows, plus headers and linking/runtime notes; consumers still provide external dependencies. The single TypeScript ZIP contains the compiled package. Go consumers use the immutable module tag through `go get`; there is no general-purpose compiled Go library asset. The nested module uses `github.com/zfogg/gotel/go` and matching `go/vX.Y.Z` tags. Publish the nested Go tag at the exact release commit after preflight succeeds. Python releases include platform wheels and an sdist, built from the uv project in `python/`; GitHub publication does not publish to PyPI. GitHub Release publication does not publish to the npm registry.

Treat version tags as immutable because Go module consumers resolve them by version. Rerun an unchanged workflow if a runner or network failure was transient. If a code fix is needed after tagging, publish the fix under a new patch version. Remove incomplete or test GitHub Release entries after verifying they are unusable; preserve valid historical releases. Do not repeatedly retry a persistent failure without fixing its cause.

# Update consumers

The repo base is `~/src/` on Windows and `~/src/github.com/zfogg/` on macOS and Linux. Known consumers under that base are `ascii-chat` (Git submodule `deps/ascii-chat-deps/gotel`), `spaniel` (`go.mod`), `ratio` (`go.mod` and `web/package.json`), `stonkpile` (`go.mod` and `web/package.json`), and `beanaries` (`apps/backend` uv workspace and `apps/web/package.json`). Check for additional consumers when doing a rollout; this list may grow. Respect each repository's instructions and existing work. In Go consumers, update the module to the published tag, tidy modules if needed, and run focused tests or builds. In ascii-chat, advance the submodule to the release tag, configure/build with its prescribed CMake preset, and test the affected integration. Commit and push consumer updates when the user requests a rollout. Deploy running services separately when requested, then verify their version and telemetry.


For Beanaries, update the gotel Git source in `apps/backend/pyproject.toml`
(`subdirectory = "python"`) and the TypeScript dependency in
`apps/web/package.json` to the published release commit. Run
`uv lock --directory apps/backend` and `pnpm install --lockfile-only` from the
repository root, committing `apps/backend/uv.lock` and `pnpm-lock.yaml` along
with both manifests. Run focused backend and browser telemetry tests. For a
live rollout, exercise the website and check Beanaries' Spaniel HTTP API for a
`beanaries-web` root with a `beanaries-api` child, Keycloak server descendants,
and connected Temporal workflow/activity spans on the scraper worker. Use
Beanaries' `scripts/verify_telemetry_traces.py` with captured browser and worker
trace IDs to verify complete ancestry and absence of error spans.
