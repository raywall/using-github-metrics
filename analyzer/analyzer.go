package analyzer

import (
	"context"
	"sync"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// NewAnalyzer creates a new Analyzer instance with an authenticated GitHub client.
func NewAnalyzer(owner, defaultBranch, workflowID string, startDate, endDate time.Time, token string, projects map[string][]string) *Analyzer {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	return &Analyzer{
		Owner:         owner,
		DefaultBranch: defaultBranch,
		WorkflowID:    workflowID,
		StartDate:     startDate,
		EndDate:       endDate,
		Token:         token,
		Projects:      projects,
		client:        client,
	}
}

// Check computes all metrics for all repos sequentially, but metrics per repo in parallel.
func (a *Analyzer) Check(ctx context.Context) ([]RepoMetrics, error) {
	var metrics []RepoMetrics

	// Flatten all repos from projects
	var allRepos []string
	for _, repos := range a.Projects {
		allRepos = append(allRepos, repos...)
	}

	for _, repo := range allRepos {
		m := RepoMetrics{Repo: repo}

		var wg sync.WaitGroup

		// Launch goroutines for each metric
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.UniqueContributors, m.ContributorsList, _ = a.GetUniqueContributors(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.CommitDist, _ = a.GetCommitDistribution(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ConflictRate, m.ConflictMergesCount, _ = a.GetConflictRateAndCount(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.AvgMergeTimeDays, _ = a.GetAvgMergeTime(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.AvgReviewersPerPR, m.CrossTeamReviews, _ = a.GetAvgReviewersPerPR(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ChurnByFile, _ = a.GetChurnByFile(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.ChurnByDir, _ = a.GetChurnByDir(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IntegrationIssues, _ = a.GetIntegrationIssues(ctx, repo) // ← função não mostrada ainda
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RevertRate, _ = a.GetRevertRate(ctx, repo) // ← função não mostrada
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.MainBranchSizeBytes, m.MainFileCount, _ = a.GetMainSize(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.SuccessfulReruns, _ = a.GetSuccessfulReruns(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RollbackIssues, _ = a.GetRollbackIssues(ctx, repo)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.WorkflowFailures, _ = a.GetWorkflowFailures(ctx, repo) // ← função não mostrada
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.SuccessfulDeploys, _ = a.GetSuccessfulDeploys(ctx, repo) // ← função não mostrada
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.AvgThreadDepth, _ = a.GetAvgThreadDepth(ctx, repo)
		}()

		wg.Wait()

		metrics = append(metrics, m)
	}

	return metrics, nil
}
