package analyzer

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/go-github/v62/github"
)

// GetAvgMergeTime returns the average merge time in days for PRs in the period.
func (a *Analyzer) GetAvgMergeTime(ctx context.Context, repo string) (float64, error) {
	opts := &github.PullRequestListOptions{
		State:       "closed",
		Sort:        "created",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var allPRs []*github.PullRequest

	for {
		prs, resp, err := a.client.PullRequests.List(ctx, a.Owner, repo, opts)
		if err != nil {
			return 0, err
		}
		for _, pr := range prs {
			if pr.CreatedAt.Time.After(a.StartDate) && pr.CreatedAt.Time.Before(a.EndDate) {
				allPRs = append(allPRs, pr)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	var totalDuration time.Duration
	count := 0
	for _, pr := range allPRs {
		if pr.MergedAt != nil {
			delta := pr.MergedAt.Time.Sub(pr.CreatedAt.Time)
			totalDuration += delta
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	return totalDuration.Hours() / float64(count*24), nil
}

// GetAvgReviewersPerPR returns the average number of reviewers per PR and cross-team reviews.
// For cross-team, this is a placeholder; implement with a user-to-team map if available.
func (a *Analyzer) GetAvgReviewersPerPR(ctx context.Context, repo string) (float64, int, error) {
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

	totalReviewers := 0
	crossTeam := 0 // Placeholder for cross-team count
	countPRs := 0
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	sem := make(chan struct{}, 10)
	for _, pr := range allPRs {
		wg.Add(1)
		go func(prNum int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			reviews, resp, err := a.client.PullRequests.ListReviews(ctx, a.Owner, repo, prNum, &github.ListOptions{PerPage: 100})
			if err == nil {
				uniqueReviewers := make(map[string]struct{})
				for _, r := range reviews {
					if r.User != nil {
						uniqueReviewers[*r.User.Login] = struct{}{}
					}
				}
				mu.Lock()
				totalReviewers += len(uniqueReviewers)
				countPRs++
				mu.Unlock()
			}
			a.checkRateLimit(resp)
		}(*pr.Number)
	}
	wg.Wait()

	if countPRs == 0 {
		return 0, 0, nil
	}
	avg := float64(totalReviewers) / float64(countPRs)
	return avg, crossTeam, nil
}

// GetSuccessfulReruns returns the count of successful workflow re-runs in the period.
func (a *Analyzer) GetSuccessfulReruns(ctx context.Context, repo string) (int, error) {
	workflowIDInt, err := strconv.ParseInt(a.WorkflowID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid workflow ID: %v", err)
	}
	opts := &github.ListWorkflowRunsOptions{Created: fmt.Sprintf("%s..%s", a.StartDate.Format("2006-01-02"), a.EndDate.Format("2006-01-02")), ListOptions: github.ListOptions{PerPage: 100}}
	count := 0
	for {
		runs, resp, err := a.client.Actions.ListWorkflowRunsByID(ctx, a.Owner, repo, workflowIDInt, opts)
		if err != nil {
			return 0, err
		}
		for _, run := range runs.WorkflowRuns {
			if *run.Conclusion == "success" && *run.RunAttempt > 1 {
				count++
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}
	return count, nil
}

// GetRollbackIssues returns the count of issues with rollback label in the period.
func (a *Analyzer) GetRollbackIssues(ctx context.Context, repo string) (int, error) {
	opts := &github.IssueListByRepoOptions{Labels: []string{"rollback"}, Since: a.StartDate, State: "all", ListOptions: github.ListOptions{PerPage: 100}}
	count := 0
	for {
		issues, resp, err := a.client.Issues.ListByRepo(ctx, a.Owner, repo, opts)
		if err != nil {
			return 0, err
		}
		for _, i := range issues {
			if i.CreatedAt.Before(a.EndDate) {
				count++
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}
	return count, nil
}

// GetAvgThreadDepth returns the average thread depth for issues/PRs in the period.
func (a *Analyzer) GetAvgThreadDepth(ctx context.Context, repo string) (float64, error) {
	// List issues
	issueOpts := &github.IssueListByRepoOptions{Since: a.StartDate, State: "all", ListOptions: github.ListOptions{PerPage: 100}}
	var allIssues []*github.Issue
	for {
		issues, resp, err := a.client.Issues.ListByRepo(ctx, a.Owner, repo, issueOpts)
		if err != nil {
			return 0, err
		}
		for _, i := range issues {
			if i.CreatedAt.Before(a.EndDate) {
				allIssues = append(allIssues, i)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		issueOpts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	// List PRs (similar to issues for comments)
	prOpts := &github.PullRequestListOptions{State: "all", Sort: "created", Direction: "desc", ListOptions: github.ListOptions{PerPage: 100}}
	var allPRs []*github.PullRequest
	for {
		prs, resp, err := a.client.PullRequests.List(ctx, a.Owner, repo, prOpts)
		if err != nil {
			return 0, err
		}
		for _, pr := range prs {
			if pr.CreatedAt.After(a.StartDate) && pr.CreatedAt.Before(a.EndDate) {
				allPRs = append(allPRs, pr)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		prOpts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	totalComments := 0
	totalItems := len(allIssues) + len(allPRs)
	if totalItems == 0 {
		return 0, nil
	}

	var mu sync.Mutex
	wg := sync.WaitGroup{}
	sem := make(chan struct{}, 10)

	// For issues
	for _, issue := range allIssues {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			comments, resp, err := a.client.Issues.ListComments(ctx, a.Owner, repo, num, &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}})
			if err == nil {
				mu.Lock()
				totalComments += len(comments)
				mu.Unlock()
			}
			a.checkRateLimit(resp)
		}(*issue.Number)
	}

	// For PRs
	for _, pr := range allPRs {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			comments, resp, err := a.client.PullRequests.ListComments(ctx, a.Owner, repo, num, &github.PullRequestListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}})
			if err == nil {
				mu.Lock()
				totalComments += len(comments)
				mu.Unlock()
			}
			a.checkRateLimit(resp)
		}(*pr.Number)
	}

	wg.Wait()

	return float64(totalComments) / float64(totalItems), nil
}
