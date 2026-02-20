package git

import (
	"context"
)

type PR struct {
	ID      int    `json:"id"`
	URL     string `json:"url"`
	Number  int    `json:"number"`
	Head    string `json:"head"`
	Base    string `json:"base"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

type Diff struct {
	Files []FileDiff `json:"files"`
}

type FileDiff struct {
	Filename string `json:"filename"`
	Patch    string `json:"patch"`
	Status   string `json:"status"`
}

type ReviewComment struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

type Provider interface {
	OpenPR(ctx context.Context, title, body, head, base string) (PR, error)
	MergePR(ctx context.Context, prNumber int) error
	PostComment(ctx context.Context, prNumber int, comment ReviewComment) error
	GetDiff(ctx context.Context, prNumber int) (Diff, error)
}
