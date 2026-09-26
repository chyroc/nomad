# @chyroc/nomad

Self-hosted coding agent CLI for the [Volcengine Ark managed-agents](https://www.volcengine.com/product/ark) runtime.

Installing this package downloads the platform-specific prebuilt `nomad`
binary from the matching [GitHub Release](https://github.com/chyroc/nomad/releases).
No binary is bundled in the npm package.

## Install

```bash
npm install -g @chyroc/nomad
# or run without installing
npx @chyroc/nomad
```

Supported platforms: macOS / Linux / FreeBSD on x64 and arm64.

`postinstall` downloads the archive from `github.com`. On networks that
block the GitHub release host, set `NOMAD_DOWNLOAD_BASE_URL` to a mirror
serving the same `nomad_<version>_<os>_<arch>.tar.gz` layout, or install
from source with `go install github.com/chyroc/nomad/cmd/nomad@latest`.

## Usage

```bash
nomad
nomad -p "explain this repo"
```

See the [project README](https://github.com/chyroc/nomad#readme) for setup
(OAuth login or an existing arkcli session), commands, and configuration.
