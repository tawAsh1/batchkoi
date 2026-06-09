# batchkoi 🎣

> **バッチこい！** — a minimal deployment tool for **AWS Batch** job definitions.

`batchkoi` is to AWS Batch what [ecspresso](https://github.com/kayac/ecspresso) is to ECS and
[lambroll](https://github.com/fujiwara/lambroll) is to Lambda: a single-binary CLI that manages
your **Batch job definitions as code** — render, diff, register, deploy, and run them straight
from a Jsonnet/JSON file that mirrors the AWS API.

> ⚠️ **Status: early WIP (v0).** `render`, `diff`, `register`, `deploy`, and `deregister` are
> implemented (the AWS round-trip is not yet battle-tested); `run` / `verify` / `status` / `init`
> are next. Scope is intentionally **job definitions only** (see Design).

## Why

AWS Batch job definitions are *versioned revisions* — every image-tag bump creates a new one,
exactly like ECS task definitions. Managing that churn in Terraform is awkward (AWS's own docs
recommend a `local-exec` hack to deregister stale revisions). `batchkoi` owns that lifecycle so
your IaC doesn't have to.

## Install

```sh
# (coming soon) brew install tawAsh1/tap/batchkoi
go install github.com/tawAsh1/batchkoi@latest
```

## Quickstart

```sh
batchkoi render                 # render the job definition to JSON
batchkoi diff                   # diff vs. the latest registered revision
batchkoi register               # register a new revision
batchkoi deploy                 # register only if changed, then prune old revisions
batchkoi deploy --keep-count 5  # ...keeping only the 5 newest active revisions
batchkoi deploy -o json         # machine-readable output
batchkoi run                    # submit a job + tail logs                 (WIP)
```

By default batchkoi reads `batchkoi.yml` in the current directory (override with `-c`).

## Configuration

Two files, like ecspresso — a tool config and the job definition:

```yaml
# batchkoi.yml
region: ap-northeast-1
job_definition: jobdef.jsonnet
plugins:
  - name: tfstate
    config:
      path: terraform.tfstate
```

```jsonnet
// jobdef.jsonnet — renders to the AWS Batch RegisterJobDefinition shape
local env = std.native('env');
local tfstate = std.native('tfstate');
{
  jobDefinitionName: 'myapp-' + env('APP_ENV', 'dev'),
  type: 'container',
  platformCapabilities: ['FARGATE'],
  containerProperties: {
    image: 'myapp:' + env('IMAGE_TAG', 'latest'),        // injected by CI
    executionRoleArn: tfstate('aws_iam_role.batch_exec.arn'),
    resourceRequirements: [
      { type: 'VCPU', value: '0.25' },
      { type: 'MEMORY', value: '512' },
    ],
  },
}
```

**Native functions** (reached via `std.native(...)`):

| function | needs plugin | what |
|---|---|---|
| `env(name, default)` | — | environment variable, with a default |
| `must_env(name)` | — | required environment variable (errors if unset) |
| `tfstate(addr)` | `tfstate` | value from Terraform state ([tfstate-lookup](https://github.com/fujiwara/tfstate-lookup)) |
| `ssm(name)` | `ssm` | SSM parameter, resolved at render time |

See [`_example/`](_example/) for a complete runnable example:
`batchkoi -c _example/batchkoi.yml render`.

## Deploy & retention

`deploy` registers a new revision **only when the rendered definition differs** from the latest
one (otherwise it's a no-op), then optionally prunes old revisions:

```sh
batchkoi deploy --keep-count 5                       # keep the 5 newest ACTIVE revisions
batchkoi deploy --keep-count 5 --keep-revision 3,7   # ...but never deregister 3 or 7
batchkoi deregister --keep-count 3                   # prune only, without registering
```

- `--keep-count N` — keep the N most recent ACTIVE revisions (the just-registered one counts).
- `--keep-revision N` — revision(s) to always protect (repeatable / comma-separated).
- Without these flags nothing is ever deregistered (safe by default).

Add `-o json` / `--output json` to any command for machine-readable output (CI-friendly).

## Commands

| command | what it does | status |
|---|---|---|
| `render` | evaluate the config and print JSON | ✅ |
| `diff` | diff local config vs. latest registered revision | ✅ |
| `register` | register a new job definition revision | ✅ |
| `deploy` | register (only if changed) + prune old revisions | ✅ |
| `deregister` | deregister old revisions per keep policy | ✅ |
| `run` | submit a job and tail its CloudWatch logs | 🚧 |
| `verify` | check image / IAM roles / log group | 🚧 |
| `status` | list registered revisions | 🚧 |
| `init` | generate config from an existing job definition | 🚧 |

## Design

- **Job definitions only.** Compute Environments and Job Queues are *referenced*, not managed —
  keep those in Terraform/CDK. batchkoi focuses on the thing that changes every deploy.
- **Config mirrors the API.** The Jsonnet/JSON renders directly into the Batch
  `RegisterJobDefinition` request shape — no bespoke schema to learn.
- **No "rollback to stable".** Unlike ECS services, Batch jobs are ephemeral; there's no running
  service to converge or health-check. A "deploy" simply means *register a new revision*.

## Acknowledgements

batchkoi stands on the shoulders of [fujiwara](https://github.com/fujiwara)'s deployment tools.
It is directly inspired by [**lambroll**](https://github.com/fujiwara/lambroll) (AWS Lambda) and
[**ecspresso**](https://github.com/kayac/ecspresso) (Amazon ECS) — the config-as-API-shape design,
the Jsonnet templating with `env` / `tfstate` native functions, and the overall CLI ergonomics all
follow their lead. It also builds directly on [tfstate-lookup](https://github.com/fujiwara/tfstate-lookup).

Many thanks to those projects and their authors. 🙏

## License

MIT
