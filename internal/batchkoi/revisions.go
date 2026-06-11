package batchkoi

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type RevisionsCmd struct {
	Active bool `name:"active" help:"Show only ACTIVE revisions."`
}

// RevisionInfo is one row of `batchkoi revisions`.
type RevisionInfo struct {
	Revision         int32             `json:"revision"`
	Status           string            `json:"status"`
	Latest           bool              `json:"latest,omitempty"` // highest ACTIVE revision
	Image            string            `json:"image,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	JobDefinitionArn string            `json:"jobDefinitionArn"`
}

// RevisionsResult is the outcome of `batchkoi revisions`.
type RevisionsResult struct {
	JobDefinitionName string         `json:"jobDefinitionName"`
	Revisions         []RevisionInfo `json:"revisions"`
}

func (r RevisionsResult) String() string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 2, 8, 2, ' ', 0)
	fmt.Fprintln(w, "REVISION\tSTATUS\tIMAGE\tTAGS")
	for _, rev := range r.Revisions {
		status := rev.Status
		if rev.Latest {
			status += " (latest)"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", rev.Revision, status, rev.Image, tagsString(rev.Tags))
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func (c *RevisionsCmd) Run(app *App) error {
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

	status := ""
	if c.Active {
		status = "ACTIVE"
	}
	revs, err := app.listRevisions(name, status)
	if err != nil {
		return err
	}

	// Revisions starts empty (not nil) so -o json emits [] when none exist.
	res := &RevisionsResult{JobDefinitionName: name, Revisions: []RevisionInfo{}}
	latestMarked := false
	for _, jd := range revs {
		info := RevisionInfo{
			Revision:         aws.ToInt32(jd.Revision),
			Status:           aws.ToString(jd.Status),
			JobDefinitionArn: aws.ToString(jd.JobDefinitionArn),
		}
		if !latestMarked && info.Status == "ACTIVE" {
			info.Latest = true
			latestMarked = true
		}
		if jd.ContainerProperties != nil {
			info.Image = aws.ToString(jd.ContainerProperties.Image)
		}
		info.Tags = jd.Tags
		res.Revisions = append(res.Revisions, info)
	}
	return app.emit(res)
}

// tagsString renders tags as "k=v,k=v" with sorted keys ("-" when none).
func tagsString(tags map[string]string) string {
	if len(tags) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + tags[k]
	}
	return strings.Join(parts, ",")
}
