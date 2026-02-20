package github

import (
	"context"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"

	portgit "github.com/alanyang/agent-mesh/internal/port/git"
)

var _ portgit.Provider = (*Client)(nil)

type Client struct {
	gh    *github.Client
	owner string
	repo  string
}

func NewClient(token, owner, repo string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(context.Background(), ts)
	return &Client{
		gh:    github.NewClient(httpClient),
		owner: owner,
		repo:  repo,
	}
}

func (c *Client) OpenPR(ctx context.Context, title, body, head, base string) (portgit.PR, error) {
	pr, _, err := c.gh.PullRequests.Create(ctx, c.owner, c.repo, &github.NewPullRequest{
		Title: github.String(title),
		Body:  github.String(body),
		Head:  github.String(head),
		Base:  github.String(base),
	})
	if err != nil {
		return portgit.PR{}, err
	}
	return portgit.PR{
		ID:     int(pr.GetID()),
		URL:    pr.GetHTMLURL(),
		Number: pr.GetNumber(),
		Head:   pr.GetHead().GetRef(),
		Base:   pr.GetBase().GetRef(),
		Title:  pr.GetTitle(),
		Body:   pr.GetBody(),
	}, nil
}

func (c *Client) MergePR(ctx context.Context, prNumber int) error {
	_, _, err := c.gh.PullRequests.Merge(ctx, c.owner, c.repo, prNumber, "", &github.PullRequestOptions{
		MergeMethod: "merge",
	})
	return err
}

func (c *Client) PostComment(ctx context.Context, prNumber int, comment portgit.ReviewComment) error {
	_, _, err := c.gh.PullRequests.CreateComment(ctx, c.owner, c.repo, prNumber, &github.PullRequestComment{
		Path: github.String(comment.File),
		Line: github.Int(comment.Line),
		Body: github.String(comment.Body),
	})
	return err
}

func (c *Client) GetDiff(ctx context.Context, prNumber int) (portgit.Diff, error) {
	var allFiles []*github.CommitFile
	opts := &github.ListOptions{PerPage: 100}
	for {
		files, resp, err := c.gh.PullRequests.ListFiles(ctx, c.owner, c.repo, prNumber, opts)
		if err != nil {
			return portgit.Diff{}, err
		}
		allFiles = append(allFiles, files...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	diffs := make([]portgit.FileDiff, len(allFiles))
	for i, f := range allFiles {
		diffs[i] = portgit.FileDiff{
			Filename: f.GetFilename(),
			Patch:    f.GetPatch(),
			Status:   f.GetStatus(),
		}
	}
	return portgit.Diff{Files: diffs}, nil
}
