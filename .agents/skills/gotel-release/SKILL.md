---
name: gotel-release
description: Release gotel's C, Go, and TypeScript packages and update projects that consume gotel. Use for versioned GitHub releases or coordinated gotel dependency rollouts.
---

# Release gotel

The version is shared by `c-gotel/CMakeLists.txt`, `package.json`, and the Git tag. The release workflow is in [`.github/workflows/release.yml`](../../../.github/workflows/release.yml); its packaging details are in [`scripts/package_release.py`](../../../scripts/package_release.py). Read those files when changing the release format.

Before tagging, check the current branch, working tree, existing tags and releases. Bump the two package versions together, run relevant local checks, commit and push. Run the workflow manually with `gh workflow run release.yml -f tag=vX.Y.Z`; this preflight builds and tests C static archives on Linux, macOS, and Windows, checks Go and TypeScript, and uploads temporary Actions artifacts without publishing a release. Require every job to pass before pushing an annotated `vX.Y.Z` tag. The tag-triggered workflow publishes the GitHub Release. Verify the release assets and `SHA256SUMS`, and confirm the latest-release link resolves to the new version.

The C ZIPs contain platform-specific static `c-gotel` and `otlp-c` archives, headers, and linking notes; consumers still link their external dependencies. The TypeScript `.tgz` is the installable npm package, and its ZIP is a convenient compiled snapshot. Go consumers use the immutable module tag through `go get`; there is no general-purpose compiled Go library asset. GitHub Release publication does not publish to the npm registry.

Treat version tags as immutable because Go module consumers resolve them by version. Rerun an unchanged workflow if a runner or network failure was transient. If a code fix is needed after tagging, publish the fix under a new patch version. Remove incomplete or test GitHub Release entries after verifying they are unusable; preserve valid historical releases. Do not repeatedly retry a persistent failure without fixing its cause.

# Update consumers

Known local consumers are `~/src/ascii-chat` (Git submodule `deps/ascii-chat-deps/gotel`), `~/src/spaniel` (`go.mod`), and `~/src/ratio` (`go.mod`). Check for additional consumers when doing a rollout; this list may grow. Respect each repository's instructions and existing work. In Go consumers, update the module to the published tag, tidy modules if needed, and run focused tests or builds. In ascii-chat, advance the submodule to the release tag, configure/build with its prescribed CMake preset, and test the affected integration. Commit and push consumer updates when the user requests a rollout. Deploy running services separately when requested, then verify their version and telemetry.
