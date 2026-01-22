package analyzer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/go-github/v62/github"
)

// GetIntegrationIssues returns the number of integration issues in the period.
func (a *Analyzer) GetIntegrationIssues(ctx context.Context, repo string) (int, error) {
	opts := &github.IssueListByRepoOptions{Labels: []string{"bug-integration"}, Since: a.StartDate, State: "all", ListOptions: github.ListOptions{PerPage: 100}}
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

// GetRevertRate returns the rate of revert commits in the period.
func (a *Analyzer) GetRevertRate(ctx context.Context, repo string) (float64, error) {
	opts := &github.CommitsListOptions{
		Since:       a.StartDate,
		Until:       a.EndDate,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	totalCommits := 0
	reverts := 0

	for {
		commits, resp, err := a.client.Repositories.ListCommits(ctx, a.Owner, repo, opts)
		if err != nil {
			return 0, err
		}

		totalCommits += len(commits)

		for _, c := range commits {
			if c.Commit != nil && c.Commit.Message != nil {
				if strings.Contains(strings.ToLower(*c.Commit.Message), "revert") {
					reverts++
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	if totalCommits == 0 {
		return 0, nil
	}

	return float64(reverts) / float64(totalCommits) * 100, nil
}

// GetWorkflowFailures returns the count of workflow failures in the period.
func (a *Analyzer) GetWorkflowFailures(ctx context.Context, repo string) (int, error) {
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
			if *run.Conclusion == "failure" {
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

// GetSuccessfulDeploys returns the count of successful deploys (runs with success and attempt==1).
func (a *Analyzer) GetSuccessfulDeploys(ctx context.Context, repo string) (int, error) {
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
			if *run.Conclusion == "success" && *run.RunAttempt == 1 {
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
