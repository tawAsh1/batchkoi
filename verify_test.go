package main

import "testing"

func TestEcrImageRe(t *testing.T) {
	cases := []struct {
		image                              string
		match                              bool
		account, region, repo, tag, digest string
	}{
		{
			image: "123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/myapp:v1.2.3",
			match: true, account: "123456789012", region: "ap-northeast-1", repo: "myapp", tag: "v1.2.3",
		},
		{
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/myapp",
			match: true, account: "123456789012", region: "us-east-1", repo: "team/myapp",
		},
		{
			image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/myapp@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			match: true, account: "123456789012", region: "us-east-1", repo: "myapp",
			digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{image: "public.ecr.aws/amazonlinux/amazonlinux:latest", match: false},
		{image: "myapp:latest", match: false},
		{image: "ghcr.io/foo/bar:1", match: false},
	}
	for _, tc := range cases {
		m := ecrImageRe.FindStringSubmatch(tc.image)
		if (m != nil) != tc.match {
			t.Errorf("%s: match = %v, want %v", tc.image, m != nil, tc.match)
			continue
		}
		if m == nil {
			continue
		}
		got := []string{m[1], m[2], m[3], m[4], m[5]}
		want := []string{tc.account, tc.region, tc.repo, tc.tag, tc.digest}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: group %d = %q, want %q", tc.image, i+1, got[i], want[i])
			}
		}
	}
}
