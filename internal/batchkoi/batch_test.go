package batchkoi

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

func TestMaxRevision(t *testing.T) {
	if got := maxRevision(nil); got != 0 {
		t.Errorf("empty: got %d, want 0", got)
	}
	revs := []types.JobDefinition{
		{Revision: aws.Int32(3)},
		{Revision: aws.Int32(7)}, // INACTIVE revisions count too
		{Revision: aws.Int32(5)},
	}
	if got := maxRevision(revs); got != 7 {
		t.Errorf("got %d, want 7", got)
	}
}

func TestTagsString(t *testing.T) {
	if got := tagsString(nil); got != "-" {
		t.Errorf("nil: got %q, want -", got)
	}
	got := tagsString(map[string]string{"Env": "prod", "App": "koi"})
	if got != "App=koi,Env=prod" {
		t.Errorf("got %q, want App=koi,Env=prod (sorted)", got)
	}
}
