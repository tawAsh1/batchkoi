# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub private vulnerability reporting](https://github.com/tawAsh1/batchkoi/security/advisories/new)
— do not open a public issue. You should get a first response within a week.

## Supported versions

Only the latest release receives security fixes.

## Supply chain

- Release binaries are built by GitHub Actions from a `v*` tag, with
  [build provenance attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations).
  Verify a download with:

  ```sh
  gh attestation verify batchkoi_*.tar.gz --repo tawAsh1/batchkoi
  ```

- All workflow actions are pinned to full commit SHAs; dependency updates go
  through Dependabot with a 7-day cooldown.
