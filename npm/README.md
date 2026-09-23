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

## Usage

```bash
nomad
nomad -p "explain this repo"
```

See the [project README](https://github.com/chyroc/nomad#readme) for setup
(OAuth login or an existing arkcli session), commands, and configuration.
