# Hermetic Build Support

This directory contains files used by Konflux/cachi2 to enable hermetic builds
(no network access during container build). All dependencies—Go modules and
RPMs—are prefetched before the build starts.

## Directory Contents

| File | Purpose |
|------|---------|
| `rpms.in.yaml` | Declares which RPM packages to prefetch and which repos to use |
| `rpms.lock.yaml` | Lockfile with resolved RPM URLs and checksums (generated) |
| `hummingbird.repo` | Yum repo definitions for Hummingbird (referenced by `rpms.in.yaml`) |

## How It Works

The Tekton pipelines in `.tekton/` specify hermetic build parameters:

```yaml
hermetic: "true"
prefetch-input: '[{"type": "gomod", "path": "."}, {"type": "rpm", "path": "hermetic"}]'
```

Before the container build, cachi2 prefetches:
1. **Go modules** from `go.mod` / `go.sum` at the repo root
2. **RPM packages** from `hermetic/rpms.lock.yaml`

During the build, network access is blocked. cachi2 injects a
`/cachi2/cachi2.env` file that redirects `go mod download` and `dnf5` to use
the prefetched dependencies.

## FIPS Images

The Dockerfile currently uses non-FIPS HI images (`hi/go:latest-builder` and
`hi/go:latest`). The FIPS variants (`latest-fips-builder` / `latest-fips`) are
pinned to Go 1.25.9 which has known CVEs. Switch to FIPS images once updated
versions are available.

## Regenerating the RPM Lockfile

When you add/remove RPM packages in `rpms.in.yaml` or update `hummingbird.repo`,
regenerate `rpms.lock.yaml`:

### 1. Build the lockfile generator container (one-time setup)

```bash
git clone https://github.com/konflux-ci/rpm-lockfile-prototype.git
cd rpm-lockfile-prototype
podman build -t localhost/rpm-lockfile-prototype .
```

### 2. Generate the lockfile

From the repo root:

```bash
podman run --rm \
  -v "$HOME/.config/containers/auth.json:/root/.docker/config.json:ro" \
  -v "$(pwd):/workdir:z" \
  -w /workdir \
  localhost/rpm-lockfile-prototype \
  --outfile hermetic/rpms.lock.yaml \
  hermetic/rpms.in.yaml
```

The auth.json mount is needed so skopeo can pull the base image metadata from
the container registry.
