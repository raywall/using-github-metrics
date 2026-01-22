package analyzer

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/google/go-github/v62/github"
)

// GetMainSize returns the size and file count of the default branch.
func (a *Analyzer) GetMainSize(ctx context.Context, repo string) (int64, int, error) {
	ref, _, err := a.client.Git.GetRef(ctx, a.Owner, repo, "heads/"+a.DefaultBranch)
	if err != nil {
		return 0, 0, err
	}

	commitSHA := *ref.Object.SHA

	tree, resp, err := a.client.Git.GetTree(ctx, a.Owner, repo, commitSHA, true) // true = recursive
	if err != nil {
		return 0, 0, err
	}
	a.checkRateLimit(resp)

	var totalSize int64
	fileCount := 0

	for _, entry := range tree.Entries {
		if entry != nil && entry.Type != nil && *entry.Type == "blob" {
			if entry.Size != nil {
				totalSize += int64(*entry.Size) // ← cast int → int64
			}
			fileCount++
		}
	}

	return totalSize, fileCount, nil
}

// GetCommitDistribution returns the distribution of commits by contributor for a repo in the period.
func (a *Analyzer) GetCommitDistribution(ctx context.Context, repo string) (map[string]int, error) {
	opts := &github.CommitsListOptions{
		Since:       a.StartDate,
		Until:       a.EndDate,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	dist := make(map[string]int)

	for {
		commits, resp, err := a.client.Repositories.ListCommits(ctx, a.Owner, repo, opts)
		if err != nil {
			return nil, err
		}

		for _, c := range commits {
			if c.Author != nil && c.Author.Login != nil {
				dist[*c.Author.Login]++
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	return dist, nil
}

// GetConflictRateAndCount returns the rate and count of PRs with merge conflicts for a repo in the period.
func (a *Analyzer) GetConflictRateAndCount(ctx context.Context, repo string) (float64, int, error) {
	opts := &github.PullRequestListOptions{State: "all", Sort: "created", Direction: "desc", ListOptions: github.ListOptions{PerPage: 100}}
	var allPRs []*github.PullRequest
	for {
		prs, resp, err := a.client.PullRequests.List(ctx, a.Owner, repo, opts)
		if err != nil {
			return 0, 0, err
		}
		for _, pr := range prs {
			if pr.CreatedAt.After(a.StartDate) && pr.CreatedAt.Before(a.EndDate) {
				allPRs = append(allPRs, pr)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	var mu sync.Mutex
	conflicts := 0
	wg := sync.WaitGroup{}
	sem := make(chan struct{}, 10)
	for _, pr := range allPRs {
		wg.Add(1)
		go func(pr *github.PullRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fullPR, resp, err := a.client.PullRequests.Get(ctx, a.Owner, repo, *pr.Number)
			if err == nil && fullPR.Mergeable != nil && !*fullPR.Mergeable {
				mu.Lock()
				conflicts++
				mu.Unlock()
			}
			a.checkRateLimit(resp)
		}(pr)
	}
	wg.Wait()

	if len(allPRs) == 0 {
		return 0, 0, nil
	}
	rate := float64(conflicts) / float64(len(allPRs)) * 100
	return rate, conflicts, nil
}

// GetChurnByFile returns the churn rate by file for commits in the period.
func (a *Analyzer) GetChurnByFile(ctx context.Context, repo string) (map[string]int, error) {
	commitOpts := &github.CommitsListOptions{
		Since:       a.StartDate,
		Until:       a.EndDate,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var commits []*github.RepositoryCommit

	for {
		cs, resp, err := a.client.Repositories.ListCommits(ctx, a.Owner, repo, commitOpts)
		if err != nil {
			return nil, err
		}
		commits = append(commits, cs...)
		if resp.NextPage == 0 {
			break
		}
		commitOpts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	churn := make(map[string]int)
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	sem := make(chan struct{}, 10)

	for _, c := range commits {
		if c.SHA == nil {
			continue
		}
		wg.Add(1)
		go func(sha string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// ← adicione nil como último argumento
			full, resp, err := a.client.Repositories.GetCommit(ctx, a.Owner, repo, sha, nil)
			if err == nil && full != nil && full.Files != nil {
				mu.Lock()
				for _, f := range full.Files {
					if f.Filename != nil {
						churn[*f.Filename]++
					}
				}
				mu.Unlock()
			}
			if resp != nil {
				a.checkRateLimit(resp)
			}
		}(*c.SHA)
	}
	wg.Wait()

	return churn, nil
}

// GetChurnByDir returns the churn rate by directory, derived from churn by file.
func (a *Analyzer) GetChurnByDir(ctx context.Context, repo string) (map[string]int, error) {
	churnByFile, err := a.GetChurnByFile(ctx, repo)
	if err != nil {
		return nil, err
	}
	churnByDir := make(map[string]int)
	for file, count := range churnByFile {
		dir := filepath.Dir(file)
		churnByDir[dir] += count
	}
	return churnByDir, nil
}
