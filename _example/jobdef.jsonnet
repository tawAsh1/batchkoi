// jobdef.jsonnet — an AWS Batch job definition (Fargate) for batchkoi.
// Renders to the AWS Batch RegisterJobDefinition request shape.
//
// Native functions are provided by batchkoi (see plugins in batchkoi.yml):
local env = std.native('env');
local must_env = std.native('must_env');
local tfstate = std.native('tfstate');

{
  jobDefinitionName: 'batchkoi-example-' + env('APP_ENV', 'dev'),
  type: 'container',
  platformCapabilities: ['FARGATE'],
  containerProperties: {
    image: '%s/batchkoi-example:%s' % [
      env('ECR_REGISTRY', 'public.ecr.aws/amazonlinux'),
      env('IMAGE_TAG', 'latest'),
    ],
    command: ['echo', 'batch... koi!'],
    // Pulled from terraform state at render time:
    jobRoleArn: tfstate('aws_iam_role.batch_job.arn'),
    executionRoleArn: tfstate('aws_iam_role.batch_execution.arn'),
    resourceRequirements: [
      { type: 'VCPU', value: env('VCPU', '0.25') },
      { type: 'MEMORY', value: env('MEMORY', '512') },
    ],
    networkConfiguration: {
      assignPublicIp: 'ENABLED',
    },
    fargatePlatformConfiguration: {
      platformVersion: 'LATEST',
    },
    logConfiguration: {
      logDriver: 'awslogs',
    },
    environment: [
      { name: 'APP_ENV', value: env('APP_ENV', 'dev') },
    ],
  },
  retryStrategy: {
    attempts: 1,
  },
  timeout: {
    attemptDurationSeconds: 600,
  },
}
