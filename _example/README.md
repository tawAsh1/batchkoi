# Example

A runnable Fargate job definition. Rendering needs no AWS account — the
`tfstate` plugin reads the bundled `terraform.tfstate`.

```sh
# from the repository root
batchkoi -c _example/batchkoi.yml render

# values are injected at render time
IMAGE_TAG=v1.2.3 APP_ENV=prod batchkoi -c _example/batchkoi.yml render
```

Files:

- `batchkoi.yml` — tool config: region, path to the job definition, plugins
- `jobdef.jsonnet` — the `RegisterJobDefinition` request, with `env()` /
  `tfstate()` lookups
- `terraform.tfstate` — fake state providing the IAM role ARNs

Against a real AWS account, the usual cycle is:

```sh
batchkoi -c _example/batchkoi.yml diff
batchkoi -c _example/batchkoi.yml verify
batchkoi -c _example/batchkoi.yml deploy --keep-count 5
batchkoi -c _example/batchkoi.yml run --queue <your-queue>
```
