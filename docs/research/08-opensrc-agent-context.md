# OpenSrc for Dependency Context

`opensrc` is useful as an agent-side research tool when we need to inspect the
real source code of third-party packages instead of relying only on docs,
generated typings, or examples.

## Why it fits this repo

The video pipeline depends on external tools where implementation details matter:

- HyperFrames rendering behavior, alpha output, codecs, and CLI flags.
- FFmpeg wrappers and package examples if we later add JS-based overlay tooling.
- Python/Rust packages if the parser, optical-flow cleanup, or interpolation
  stages grow beyond the current Go/FFmpeg/HLAE stack.

The important constraint is that `opensrc` should stay out of the repository. It
fetches source into a global cache and prints the resolved path, so we can search
that cache with normal tools.

## Usage

```powershell
npx --yes opensrc path hyperframes
```

Then inspect the resolved source path:

```powershell
rg "mov" $(npx --yes opensrc path hyperframes)
rg "alpha" $(npx --yes opensrc path hyperframes)
```

For non-npm sources, prefix the package registry:

```powershell
npx --yes opensrc path pypi:requests
npx --yes opensrc path crates:serde
```

## Workflow Rule

Use `opensrc` during investigation, not as a runtime dependency. If we learn
something important from a dependency source inspection, capture the conclusion
in `docs/research/` or in the relevant implementation comment, but do not commit
the downloaded source cache.
