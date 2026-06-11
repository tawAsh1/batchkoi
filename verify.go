package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	batchtypes "github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

type VerifyCmd struct{}

const (
	checkOK   = "OK"
	checkNG   = "NG"
	checkSkip = "SKIP"
)

// verifyCheck is one verification step.
type verifyCheck struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Status string `json:"status"` // OK | NG | SKIP
	Detail string `json:"detail,omitempty"`
}

// VerifyResult is the outcome of a verify.
type VerifyResult struct {
	JobDefinitionName string        `json:"jobDefinitionName"`
	Checks            []verifyCheck `json:"checks"`
}

func (r VerifyResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "verify %s\n", r.JobDefinitionName)
	ok, ng, skip := 0, 0, 0
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "  [%s] %s %s", c.Status, c.Name, c.Target)
		if c.Detail != "" {
			fmt.Fprintf(&b, " — %s", c.Detail)
		}
		fmt.Fprintln(&b)
		switch c.Status {
		case checkOK:
			ok++
		case checkNG:
			ng++
		default:
			skip++
		}
	}
	fmt.Fprintf(&b, "%d OK, %d NG, %d SKIP", ok, ng, skip)
	return b.String()
}

// Run verifies that the resources the rendered job definition points at
// actually exist before a deploy: the job queue, the IAM roles, the container
// image (when it lives in ECR), and the awslogs log group.
func (c *VerifyCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	local, err := app.loadJobDefinition()
	if err != nil {
		return err
	}
	name := aws.ToString(local.JobDefinitionName)
	if name == "" {
		return fmt.Errorf("jobDefinitionName is empty in the rendered job definition")
	}

	res := &VerifyResult{JobDefinitionName: name}
	add := func(c verifyCheck) { res.Checks = append(res.Checks, c) }

	add(app.verifyJobQueue(app.config.JobQueue))

	cp := local.ContainerProperties
	if cp == nil {
		add(verifyCheck{Name: "containerProperties", Status: checkSkip, Detail: "not set — container checks skipped"})
	} else {
		add(app.verifyRole("executionRoleArn", aws.ToString(cp.ExecutionRoleArn)))
		add(app.verifyRole("jobRoleArn", aws.ToString(cp.JobRoleArn)))
		add(app.verifyImage(aws.ToString(cp.Image)))
		add(app.verifyLogGroup(cp.LogConfiguration))
	}

	if err := app.emit(res); err != nil {
		return err
	}
	ng := 0
	for _, c := range res.Checks {
		if c.Status == checkNG {
			ng++
		}
	}
	if ng > 0 {
		return fmt.Errorf("verify failed: %d check(s) NG", ng)
	}
	return nil
}

func (app *App) verifyJobQueue(queue string) verifyCheck {
	c := verifyCheck{Name: "jobQueue", Target: queue}
	if queue == "" {
		c.Status, c.Detail = checkSkip, "job_queue not set in config"
		return c
	}
	out, err := app.batch.DescribeJobQueues(app.ctx, &batch.DescribeJobQueuesInput{
		JobQueues: []string{queue},
	})
	if err != nil {
		c.Status, c.Detail = checkNG, err.Error()
		return c
	}
	if len(out.JobQueues) == 0 {
		c.Status, c.Detail = checkNG, "not found"
		return c
	}
	q := out.JobQueues[0]
	if q.State != batchtypes.JQStateEnabled || q.Status == batchtypes.JQStatusInvalid {
		c.Status = checkNG
		c.Detail = fmt.Sprintf("state=%s status=%s %s", q.State, q.Status, aws.ToString(q.StatusReason))
		return c
	}
	c.Status = checkOK
	return c
}

func (app *App) verifyRole(field, arn string) verifyCheck {
	c := verifyCheck{Name: field, Target: arn}
	if arn == "" {
		c.Status, c.Detail = checkSkip, "not set"
		return c
	}
	// arn:aws:iam::123456789012:role/path/name — GetRole wants the bare name.
	roleName := arn[strings.LastIndex(arn, "/")+1:]
	if _, err := iam.NewFromConfig(app.awsCfg).GetRole(app.ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	}); err != nil {
		c.Status, c.Detail = checkNG, err.Error()
		return c
	}
	c.Status = checkOK
	return c
}

// ecrImageRe matches private ECR image URIs: account.dkr.ecr.region.amazonaws.com/repo[:tag][@digest]
var ecrImageRe = regexp.MustCompile(`^(\d{12})\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com/([^:@]+)(?::([^@]+))?(?:@(sha256:[0-9a-f]+))?$`)

func (app *App) verifyImage(image string) verifyCheck {
	c := verifyCheck{Name: "image", Target: image}
	if image == "" {
		c.Status, c.Detail = checkSkip, "not set"
		return c
	}
	m := ecrImageRe.FindStringSubmatch(image)
	if m == nil {
		c.Status, c.Detail = checkSkip, "not a private ECR image — existence not checked"
		return c
	}
	account, region, repo, tag, digest := m[1], m[2], m[3], m[4], m[5]
	id := ecrtypes.ImageIdentifier{}
	switch {
	case digest != "":
		id.ImageDigest = aws.String(digest)
	case tag != "":
		id.ImageTag = aws.String(tag)
	default:
		id.ImageTag = aws.String("latest")
	}
	client := ecr.NewFromConfig(app.awsCfg, func(o *ecr.Options) { o.Region = region })
	if _, err := client.DescribeImages(app.ctx, &ecr.DescribeImagesInput{
		RegistryId:     aws.String(account),
		RepositoryName: aws.String(repo),
		ImageIds:       []ecrtypes.ImageIdentifier{id},
	}); err != nil {
		c.Status, c.Detail = checkNG, err.Error()
		return c
	}
	c.Status = checkOK
	return c
}

func (app *App) verifyLogGroup(lc *batchtypes.LogConfiguration) verifyCheck {
	group := defaultLogGroup
	if lc != nil {
		if lc.LogDriver != batchtypes.LogDriverAwslogs {
			return verifyCheck{Name: "logGroup", Status: checkSkip,
				Detail: fmt.Sprintf("logDriver=%s — only awslogs is checked", lc.LogDriver)}
		}
		if g := lc.Options["awslogs-group"]; g != "" {
			group = g
		}
	}
	c := verifyCheck{Name: "logGroup", Target: group}
	out, err := app.logs.DescribeLogGroups(app.ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(group),
	})
	if err != nil {
		c.Status, c.Detail = checkNG, err.Error()
		return c
	}
	for _, g := range out.LogGroups {
		if aws.ToString(g.LogGroupName) == group {
			c.Status = checkOK
			return c
		}
	}
	c.Status, c.Detail = checkNG, "not found"
	return c
}
