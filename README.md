# batchkoi 🎣

> **バッチこい！** — a minimal deployment tool for **AWS Batch** job definitions.

`batchkoi` is to AWS Batch what [ecspresso](https://github.com/kayac/ecspresso) is to ECS and
[lambroll](https://github.com/fujiwara/lambroll) is to Lambda: a single-binary CLI that manages
your **Batch job definitions as code** — render, diff, register, deploy, and run them straight
from a Jsonnet/JSON file that mirrors the AWS API.

> ⚠️ **Status: early WIP (v0).** All commands (`init` / `render` / `diff` / `register` / `deploy` /
> `verify` / `revisions` / `rollback` / `deregister` / `run`) are implemented, but the AWS
> round-trip is not yet battle-tested. Scope is intentionally **job definitions only** (see Design).

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
batchkoi init --jd my-jobdef    # generate batchkoi.yml + jobdef.json from AWS
batchkoi render                 # render the job definition to JSON
batchkoi diff                   # diff vs. the latest registered revision
batchkoi verify                 # check queue / roles / image / log group exist
batchkoi register               # register a new revision
batchkoi register --dry-run     # show the payload + revision number it would create
batchkoi deploy                 # register only if changed, then prune old revisions
batchkoi deploy --keep-count 5  # ...keeping only the 5 newest active revisions
batchkoi deploy --keep-count 5 --dry-run  # preview, incl. what would be deregistered
batchkoi revisions              # list registered revisions (status / image)
batchkoi rollback               # deregister latest so the previous rev is latest again
batchkoi run --queue my-queue   # register, submit a job, and tail its logs
batchkoi run --rev latest -q my-queue   # ...run the latest existing revision instead
```

By default batchkoi reads `batchkoi.yml` in the current directory (override with `-c`).

## Configuration

Two files, like ecspresso — a tool config and the job definition:

```yaml
# batchkoi.yml
region: ap-northeast-1
job_definition: jobdef.jsonnet
# job_queue: my-job-queue        # default queue for `run` (or pass --queue)
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
| `caller_identity()` | — | `{Account, Arn, UserId}` from STS, e.g. `caller_identity().Account` |
| `tfstate(addr)` | `tfstate` | value from Terraform state ([tfstate-lookup](https://github.com/fujiwara/tfstate-lookup)) |
| `ssm(name)` | `ssm` | SSM parameter, resolved at render time |

Jsonnet **external variables** are set with the global `--ext-str` / `--ext-code` flags and read
with `std.extVar(...)` — handy for injecting the image tag from CI:

```sh
batchkoi deploy --ext-str IMAGE_TAG=$GIT_SHA      # std.extVar('IMAGE_TAG') in the template
batchkoi render --ext-code attempts=3             # code (numbers, objects), not a string
```

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
- `--dry-run` — preview everything: whether a new revision would be registered (and the diff),
  and exactly which revisions retention **would deregister** — counting the would-be new
  revision, just like the real deploy:

```console
$ batchkoi deploy --keep-count 3 --dry-run
would register myjob:13
--- myjob:12 (remote)
+++ myjob (local)
...
would deregister: 10, 9
would keep: 13, 12, 11
DRY RUN — nothing was changed
```

## Diff, revisions & rollback

Pin-friendly revision management:

```sh
batchkoi diff                    # vs. the latest ACTIVE revision
batchkoi diff --revision 7       # vs. a pinned revision (alias: --rev)
batchkoi diff --exit-code        # exit 1 when there are differences (like git diff) — CI guard
batchkoi revisions               # all revisions: number, status, image (latest marked)
batchkoi revisions --active      # only ACTIVE ones
batchkoi rollback                # deregister the latest ACTIVE revision
batchkoi rollback --dry-run      # ...just show what would happen
```

`revisions` shows which image each revision points at, so you can tell what a pinned revision
actually runs:

```console
$ batchkoi revisions
REVISION  STATUS           IMAGE                                    TAGS
12        ACTIVE (latest)  123456789012.dkr.ecr....com/myapp:9f3c2  deployedBy=ci,release=v1.4.0
11        ACTIVE           123456789012.dkr.ecr....com/myapp:8b21d  deployedBy=ci,release=v1.3.2
10        INACTIVE         123456789012.dkr.ecr....com/myapp:77e0c  -
```

Rollback in Batch is simple because jobs submitted by bare name resolve to the **highest ACTIVE
revision**: deregistering the latest one makes the previous revision current again.

## Init & verify

Bootstrap from what's already on AWS, and sanity-check before deploying:

```sh
batchkoi init --jd my-jobdef          # latest ACTIVE revision → batchkoi.yml + jobdef.json
batchkoi init --jd my-jobdef:7        # ...a specific revision (also accepts an ARN)
batchkoi init --jd my-jobdef --jsonnet --job-queue my-queue   # start from Jsonnet, set queue
batchkoi verify                       # job queue / IAM roles / ECR image / log group
```

`init` writes the job definition in the same canonical form `diff` uses, so a `diff` right after
`init` shows no changes. `verify` checks that everything the rendered definition points at exists
(`[OK] / [NG] / [SKIP]` per check) and exits non-zero if anything is broken — run it in CI before
`deploy`.

## Run

`run` submits a one-off job and tails its CloudWatch Logs until it finishes (exiting non-zero if
the job fails). By default it registers the local definition and runs that; point it at an
existing revision with `--revision` / `--rev`:

```sh
batchkoi run --queue my-queue                       # register local def, submit, tail logs
batchkoi run --rev latest --queue my-queue          # run the latest registered revision
batchkoi run --rev 7 --queue my-queue               # run a specific revision
batchkoi run -q my-queue --command echo --command hi  # override the container command
batchkoi run -q my-queue --no-wait                  # submit only, print the job id
```

The queue comes from `--queue`/`-q` or `job_queue:` in `batchkoi.yml` (batchkoi doesn't manage
queues — it just submits to one). Logs are read from the job's `awslogs-group` (default
`/aws/batch/job`).

Add `-o json` / `--output json` to any command for machine-readable output (CI-friendly).

## Commands

| command | what it does |
|---|---|
| `init` | generate batchkoi.yml + jobdef from an existing job definition on AWS |
| `render` | evaluate the config and print JSON |
| `diff` | diff local config vs. a registered revision (`--rev N`, `--exit-code`) |
| `verify` | check job queue / IAM roles / ECR image / log group exist |
| `register` | register a new job definition revision (`--dry-run`) |
| `deploy` | register (only if changed) + prune old revisions (`--dry-run`) |
| `revisions` | list revisions with status, image and tags |
| `rollback` | deregister the latest revision (previous becomes latest) |
| `deregister` | deregister old revisions per keep policy |
| `run` | submit a job and tail its CloudWatch logs |

## Design

- **Job definitions only.** Compute Environments and Job Queues are *referenced*, not managed —
  keep those in Terraform/CDK. batchkoi focuses on the thing that changes every deploy.
- **Config mirrors the API.** The Jsonnet/JSON renders directly into the Batch
  `RegisterJobDefinition` request shape — no bespoke schema to learn.
- **No "rollback to stable" loop.** Unlike ECS services, Batch jobs are ephemeral; there's no
  running service to converge or health-check. A "deploy" simply means *register a new revision*,
  and `rollback` simply deregisters it again.

## Acknowledgements

batchkoi stands on the shoulders of [fujiwara](https://github.com/fujiwara)'s deployment tools.
It is directly inspired by [**lambroll**](https://github.com/fujiwara/lambroll) (AWS Lambda) and
[**ecspresso**](https://github.com/kayac/ecspresso) (Amazon ECS) — the config-as-API-shape design,
the Jsonnet templating with `env` / `tfstate` native functions, and the overall CLI ergonomics all
follow their lead. It also builds directly on [tfstate-lookup](https://github.com/fujiwara/tfstate-lookup).

Many thanks to those projects and their authors. 🙏

## License

MIT
