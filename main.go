package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

const (
	// setup
	Owner        = "raywall"                  // ex: "my-org-or-username"
	RepoName     = "fast-service-toolkit"     // ex: "my-repository-name"
	WorkflowName = "2 - [DEV] Build & Deploy" // exact name of the workflow to be analyzed (do .yml ou display name)
	Branch       = "main"                     // exact branch name that will be used to check size
	MonthsBack   = 1                          // how many months you want to validate
)

var (
	token = os.Getenv("GITHUB_TOKEN") // personal Access Token with scopes: repo, workflow
	traces = make([]string, 0)
)

func init() {
	jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(jsonHandler))
}

func main() {
	ctx := context.Background()

	// authentication
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// check the branch size
	branchSize, err := getBranchSize(ctx, client)
	if err != nil {
		showMessage(fmt.Sprintf("Erro ao calcular tamanho da branch: %v", err))
		os.Exit(1)
	}

	showMessage(fmt.Sprintf("Repositório: %s/%s", Owner, RepoName))
	showMessage(fmt.Sprintf("Tamanho da branch '%s': %d KB", Branch, branchSize/1024)) // convert bytes to KB

	from := time.Now().AddDate(0, -(MonthsBack + 1), 0)
	to := time.Now().AddDate(0, -1, 0)
	showMessage(fmt.Sprintf("Período analisado: %s a %s\n", oneMonthAgo.Format("2006-01-02"), now.Format("2006-01-02")))

	// 1. average duration of the specified workflow
	showMessage("Analisando workflow durations...")
	_, avgDuration := getWorkflowDurations(ctx, client, oneMonthAgo)
	formattedAvgDuration = timeFormat(int(avgDuration))
	showMessage(fmt.Sprintf("Duração média de runs bem-sucedidos do workflow '%s': %.2f segundos\n", WorkflowName, formattedAvgDuration))

	// 2. merge conflicts (PRs with conflict resolution commits in the period)
	showMessage("Analisando conflitos de merge...")
	conflictCount := countMergeConflicts(ctx, client, oneMonthAgo)
	showMessage(fmt.Sprintf("Quantidade de merges com conflitos resolvidos no período: %d\n", conflictCount))

	// 3. rollback issues (closed with label containing 'rollback')
	showMessage("Analisando issues de rollback...")
	rollbackCount := countRollbackIssues(ctx, client, oneMonthAgo)
	showMessage(fmt.Sprintf("Quantidade de issues de rollback: %d\n", rollbackCount))

	// summary
	showMessage("Resumo:")
	showMessage(fmt.Sprintf("- Tamanho branch '%s': %d KB | Duração média workflow '%s': %.2f s", Branch, branchSize/1024, WorkflowName, formattedAvgDuration))
	showMessage(fmt.Sprintf("- Merges com conflitos resolvidos: %d", conflictCount))
	showMessage(fmt.Sprintf("- Issues de rollback: %d", rollbackCount))

	dumpOutputFile()
}

// Calculates the total size of files (blobs) in the specific branch (in bytes)
func getBranchSize(ctx context.Context, client *github.Client) (int64, error) {
	ref, _, err := client.Git.GetRef(ctx, Owner, RepoName, "heads/"+Branch)
	if err != nil {
		return 0, fmt.Errorf("erro ao obter ref da branch: %w", err)
	}

	tree, _, err := client.Git.GetTree(ctx, Owner, RepoName, *ref.Object.SHA, true) // recursive=true
	if err != nil {
		return 0, fmt.Errorf("erro ao obter tree recursivo: %w", err)
	}

	var totalSize int64
	for _, entry := range tree.Entries {
		if *entry.Type == "blob" && entry.Size != nil {
			totalSize += int64(*entry.Size)
		}
	}
	return totalSize, nil
}

func getWorkflowDurations(ctx context.Context, client *github.Client, since time.Time) ([]int64, float64) {
	// first, find the workflow ID by name
	workflows, _, err := client.Actions.ListWorkflows(ctx, Owner, RepoName, &github.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao listar workflows: %v", err)
		return nil, 0
	}

	var workflowID int64
	for _, wf := range workflows.Workflows {
		if wf.Name != nil && *wf.Name == WorkflowName {
			workflowID = *wf.ID
			break
		}
	}
	if workflowID == 0 {
		fmt.Fprintf(os.Stderr, "Workflow '%s' não encontrado", WorkflowName)
		return nil, 0
	}

	var totalDuration int64
	var count int

	opts := &github.ListOptions{PerPage: 100}
	for {
		runs, resp, err := client.Actions.ListWorkflowRunsByID(
			ctx,
			Owner,
			RepoName,
			workflowID,
			&github.ListWorkflowRunsOptions{
				Status:      "completed",
				Created:     ">" + since.Format("2006-01-02"),
				ListOptions: *opts,
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao listar runs do workflow: %v", err)
			break
		}

		for _, run := range runs.WorkflowRuns {
			if run.Conclusion != nil && *run.Conclusion == "success" &&
				run.CreatedAt != nil && run.UpdatedAt != nil {
				duration := run.UpdatedAt.Sub(run.CreatedAt.Time).Seconds()
				if duration > 0 {
					totalDuration += int64(duration)
					count++
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if count == 0 {
		return nil, 0
	}
	avg := float64(totalDuration) / float64(count)
	return []int64{}, avg // it's possible collect the individual durations if you want
}

func countMergeConflicts(ctx context.Context, client *github.Client, since time.Time) int {
	count := 0
	opts := &github.PullRequestListOptions{
		State:       "closed",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		prs, resp, err := client.PullRequests.List(ctx, Owner, RepoName, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao listar PRs: %v", err)
			break
		}

		for _, pr := range prs {
			if pr.ClosedAt == nil || pr.ClosedAt.Before(since) {
				continue // ignore PRs closed before the period
			}

			commitOpts := &github.ListOptions{PerPage: 100}
			var hasConflictInPeriod bool
			for {
				commits, cResp, err := client.PullRequests.ListCommits(ctx, Owner, RepoName, *pr.Number, commitOpts)
				if err != nil {
					break
				}

				for _, c := range commits {
					if c.Commit != nil && c.Commit.Author != nil && c.Commit.Author.Date != nil &&
						c.Commit.Author.Date.After(since) { // Commit criado no período
						msg := strings.ToLower(*c.Commit.Message)
						if strings.Contains(msg, "conflict") || strings.Contains(msg, "resolve conflict") ||
							strings.Contains(msg, "merge conflict") {
							hasConflictInPeriod = true
							break
						}
					}
				}

				if hasConflictInPeriod {
					break
				}

				if cResp.NextPage == 0 {
					break
				}
				commitOpts.Page = cResp.NextPage
			}

			if hasConflictInPeriod {
				count++
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return count
}

func countRollbackIssues(ctx context.Context, client *github.Client, since time.Time) int {
	count := 0
	opts := &github.IssueListByRepoOptions{
		State:       "closed",
		Since:       since,
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, Owner, RepoName, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao listar issues: %v", err)
			break
		}

		for _, issue := range issues {
			if issue.ClosedAt == nil || issue.ClosedAt.Before(since) {
				continue
			}

			hasRollbackLabel := false
			for _, label := range issue.Labels {
				if label.Name != nil && strings.Contains(strings.ToLower(*label.Name), "rollback") {
					hasRollbackLabel = true
					break
				}
			}

			if hasRollbackLabel {
				count++
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return count
}

func showMessage(msg string) {
	traces = append(traces, msg)
	log.Println(msg)
}

func timeFormat(totalSeconds int) string {
	var (
		hrs = totalSeconds / 3600
		min = (totalSeconds % 3600) / 60
		sec = totalSeconds / 60
	)

	return fmt.Sprintf("%02d:%02d:%02d", hrs, min, sec)
}

func dumpOutputFile() {
	if err := os.WriteFile("output.txx", []byte(strings.Join(trace, "")), 0644); err != nil {
		log.Println("error generating a trace output file")
	}
}