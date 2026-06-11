# batchkoi 🎣

**バッチこい！** — a minimal deployment tool for AWS Batch job definitions.

[![CI](https://github.com/tawAsh1/batchkoi/actions/workflows/ci.yml/badge.svg)](https://github.com/tawAsh1/batchkoi/actions/workflows/ci.yml)
[日本語 README](README.ja.md)

batchkoi is to AWS Batch what [ecspresso](https://github.com/kayac/ecspresso) is to ECS and
[lambroll](https://github.com/fujiwara/lambroll) is to Lambda: a single-binary CLI that manages
job definitions as code, from a Jsonnet/JSON file that mirrors the AWS API.

Batch job definitions are versioned revisions — every image-tag bump registers a new one, and
managing that churn in Terraform is awkward. batchkoi owns the job definition lifecycle so your
IaC doesn't have to. Scope is deliberately **job definitions only**: compute environments and job
queues stay in Terraform/CDK.

## Install

```sh
go install github.com/tawAsh1/batchkoi@latest
```

or grab a binary from [Releases](https://github.com/tawAsh1/batchkoi/releases).

## Quickstart

```sh
batchkoi init --jd my-jobdef     # generate batchkoi.yml + jobdef.json from AWS
batchkoi diff                    # local vs. latest registered revision
batchkoi verify                  # queue / IAM roles / image / secrets / log group exist?
batchkoi deploy --keep-count 5   # register if changed, keep the 5 newest revisions
batchkoi run -q my-queue         # submit a job and tail its CloudWatch logs
```

Reads `batchkoi.yml` in the current directory (`-c` to override).

## Configuration

Two files, like ecspresso — a tool config and the job definition:

```yaml
# batchkoi.yml
region: ap-northeast-1
job_definition: jobdef.jsonnet
# required_version: ">= 0.1.0"
# job_queue: my-job-queue        # default queue for `run`
plugins:
  - name: tfstate
    config: { path: terraform.tfstate }
```

```jsonnet
// jobdef.jsonnet — the AWS Batch RegisterJobDefinition request shape, 1:1
local env = std.native('env');
local tfstate = std.native('tfstate');
{
  jobDefinitionName: 'myapp',
  type: 'container',
  platformCapabilities: ['FARGATE'],
  containerProperties: {
    image: 'myapp:' + env('IMAGE_TAG', 'latest'),
    executionRoleArn: tfstate('aws_iam_role.batch_exec.arn'),
    resourceRequirements: [
      { type: 'VCPU', value: '0.25' },
      { type: 'MEMORY', value: '512' },
    ],
  },
}
```

Native functions: `env(name, default)`, `must_env(name)`, `caller_identity()` (always available),
`tfstate(addr)` and `ssm(name)` (enabled by the matching plugin). Jsonnet external variables come
from `--ext-str KEY=VALUE` / `--ext-code` (`std.extVar`). `--envfile .env` exports env files
before rendering, and every flag falls back to a `BATCHKOI_*` environment variable.

See [_example/](_example/) for a runnable example (no AWS account needed to render).

## Commands

| command | what it does |
|---|---|
| `init` | generate batchkoi.yml + jobdef from an existing job definition (`--jd name[:rev]`, `--jsonnet`) |
| `render` | evaluate the config and print JSON |
| `diff` | local vs. registered (`--rev N` to pin; `--exit-code` exits 2 on differences) |
| `verify` | check queue, IAM roles, ECR image, secrets, log group; non-zero exit on NG |
| `register` | register a new revision unconditionally (`--dry-run` previews payload + revision) |
| `deploy` | register only if changed, then prune (`--keep-count N`, `--keep-revision N`, `--dry-run`) |
| `revisions` | list revisions: status, image, tags, latest marker (`--active`) |
| `rollback` | deregister the latest ACTIVE revision so the previous one is latest again (`--dry-run`) |
| `deregister` | prune old revisions without registering |
| `run` | submit a job and tail logs; registers first only if changed (`--rev`, `--command`, `--env`, `--no-wait`) |

Notes:

- Nothing is ever deregistered unless you pass `--keep-count` / `--keep-revision`, and
  `deploy --dry-run` shows exactly which revisions would go.
- `run` exits non-zero when the job fails. `-o json` on any command gives machine-readable output.
- Rollback is just a deregister: jobs submitted by bare name resolve to the highest ACTIVE
  revision, so removing the latest makes the previous one current.

## Design

- **Job definitions only.** The thing that changes every deploy; everything else stays in IaC.
- **Config mirrors the API.** No bespoke schema — the file is the `RegisterJobDefinition` request.
- **No convergence loop.** Batch jobs are ephemeral; deploy = register a revision, nothing to
  health-check or wait for.

## Acknowledgements

Directly inspired by [fujiwara](https://github.com/fujiwara)'s
[lambroll](https://github.com/fujiwara/lambroll) and
[ecspresso](https://github.com/kayac/ecspresso) — the config model, Jsonnet native functions, and
CLI ergonomics all follow their lead. Built on
[tfstate-lookup](https://github.com/fujiwara/tfstate-lookup). 🙏

## License

MIT
