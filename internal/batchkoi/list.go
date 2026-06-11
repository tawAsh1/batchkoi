package batchkoi

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

type ListCmd struct {
	All bool `name:"all" help:"Include job definitions with no ACTIVE revision (counting INACTIVE revisions too)."`
}

// JobDefinitionSummary is one row of `batchkoi list`.
type JobDefinitionSummary struct {
	Name           string `json:"name"`
	Revisions      int    `json:"revisions"`
	LatestRevision int32  `json:"latestRevision"`
	Status         string `json:"status"` // status of the latest revision
	Image          string `json:"image,omitempty"`
}

// ListResult is the outcome of `batchkoi list`.
type ListResult struct {
	JobDefinitions []JobDefinitionSummary `json:"jobDefinitions"`
}

func (r ListResult) String() string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 2, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREVISIONS\tLATEST\tSTATUS\tIMAGE")
	for _, jd := range r.JobDefinitions {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", jd.Name, jd.Revisions, jd.LatestRevision, jd.Status, jd.Image)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// Run lists every job definition in the region, one row per name. Like init,
// it works without a config file — the listing is region-wide, not tied to
// the configured job definition.
func (c *ListCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := app.setupAWS(""); err != nil {
			return err
		}
	}

	status := "ACTIVE"
	if c.All {
		status = ""
	}
	revs, err := app.listRevisions("", status)
	if err != nil {
		return err
	}
	return app.emit(&ListResult{JobDefinitions: summarizeJobDefinitions(revs)})
}

// summarizeJobDefinitions groups revisions into one summary per name, sorted
// by name. revs must be sorted newest-first (as listRevisions returns them),
// so the first revision seen per name is the latest.
func summarizeJobDefinitions(revs []types.JobDefinition) []JobDefinitionSummary {
	byName := make(map[string]*JobDefinitionSummary)
	for _, jd := range revs {
		name := aws.ToString(jd.JobDefinitionName)
		s := byName[name]
		if s == nil {
			s = &JobDefinitionSummary{
				Name:           name,
				LatestRevision: aws.ToInt32(jd.Revision),
				Status:         aws.ToString(jd.Status),
			}
			if jd.ContainerProperties != nil {
				s.Image = aws.ToString(jd.ContainerProperties.Image)
			}
			byName[name] = s
		}
		s.Revisions++
	}

	out := make([]JobDefinitionSummary, 0, len(byName))
	for _, s := range byName {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
