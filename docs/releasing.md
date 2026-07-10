# Releasing

Releases are built entirely by `.github/workflows/release.yml`. The workflow
runs formatting, vet, and race tests, then cross-compiles four self-contained
binaries:

```text
darwin/amd64   darwin/arm64   linux/amd64   linux/arm64
```

Each archive also contains `LICENSE` and `README.md`. The workflow publishes a
single `checksums.txt` and a GitHub build-provenance attestation. Artifact names
are part of the installer contract:

```text
memsync_<version>_<os>_<arch>.tar.gz
```

For example, tag `v0.2.0` produces
`memsync_0.2.0_darwin_arm64.tar.gz`.

## Publish a release

Start from a clean, tested `main`, then create and push a semantic version tag:

```sh
git tag -s v0.2.0 -m "memsync v0.2.0"
git push origin v0.2.0
```

Tags containing a suffix, such as `v0.2.0-rc.1`, are published as GitHub
prereleases. Do not create release assets manually; rerunning the workflow
replaces assets on an existing release with the same versioned names.

## Verify a published release

The installer verifies `checksums.txt` automatically. Maintainers and users with
GitHub CLI can also verify GitHub's artifact attestation:

```sh
gh attestation verify memsync_0.2.0_darwin_arm64.tar.gz \
  --repo gregtuc/memsync
```

Before tagging, test the installer against an existing release or a temporary
fork. Its archive naming and version normalization must remain synchronized with
the release workflow.
