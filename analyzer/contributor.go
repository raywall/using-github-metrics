package analyzer

import (
	"context"

	"github.com/google/go-github/v62/github"
)

// GetUniqueContributors returns the number of unique contributors and their list for a repo in the period.
func (a *Analyzer) GetUniqueContributors(ctx context.Context, repo string) (int, []string, error) {
	commitOpts := &github.CommitsListOptions{
		Since:       a.StartDate,
		Until:       a.EndDate,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	unique := make(map[string]struct{})

	for {
		commits, resp, err := a.client.Repositories.ListCommits(ctx, a.Owner, repo, commitOpts)
		if err != nil {
			return 0, nil, err
		}

		for _, c := range commits {
			if c.Author != nil && c.Author.Login != nil {
				unique[*c.Author.Login] = struct{}{}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		commitOpts.Page = resp.NextPage
		a.checkRateLimit(resp)
	}

	usernames := getUsernames(unique)
	return len(unique), usernames, nil
}
