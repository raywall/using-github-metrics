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
	// Configurações - altere aqui
	Owner      = "raywall"              // ex: "minha-org"
	RepoName   = "fast-service-toolkit" // ex: "meu-projeto"
	MonthsBack = 1                      // Quantos meses para trás
)

var Token = os.Getenv("GITHUB_TOKEN") // Personal Access Token com escopos: repo, workflow

func main() {
	ctx := context.Background()

	// Autenticação
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: Token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	repo, _, err := client.Repositories.Get(ctx, Owner, RepoName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao obter repositório: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Repositório: %s/%s\n", Owner, RepoName)
	fmt.Printf("Tamanho do repositório: %d KB\n", *repo.Size)

	oneMonthAgo := time.Now().AddDate(0, -MonthsBack, 0)
	fmt.Printf("Período analisado: a partir de %s\n\n", oneMonthAgo.Format(time.RFC3339))

	// 1. Workflows - duração média
	fmt.Println("Analisando workflow durations...")
	_, avgDuration := getWorkflowDurations(ctx, client, oneMonthAgo)
	fmt.Printf("Duração média de workflows bem-sucedidos: %.2f segundos\n\n", avgDuration)

	// 2. Conflitos de merge (inferidos por mensagens de commit)
	fmt.Println("Analisando conflitos de merge...")
	conflictCount := countMergeConflicts(ctx, client, oneMonthAgo)
	fmt.Printf("Quantidade de PRs com indício de conflito: %d\n\n", conflictCount)

	// 3. Issues de rollback
	fmt.Println("Analisando issues de rollback...")
	rollbackCount := countRollbackIssues(ctx, client, oneMonthAgo)
	fmt.Printf("Quantidade de issues de rollback: %d\n\n", rollbackCount)

	// Resumo
	fmt.Println("Resumo:")
	fmt.Printf("- Tamanho repo: %d KB | Duração média workflow: %.2f s\n", *repo.Size, avgDuration)
	fmt.Printf("- Conflitos de merge (inferidos): %d\n", conflictCount)
	fmt.Printf("- Issues de rollback: %d\n", rollbackCount)
}

func getWorkflowDurations(ctx context.Context, client *github.Client, since time.Time) ([]int64, float64) {
	var totalDuration int64
	var count int

	opts := &github.ListOptions{PerPage: 100}
	for {
		runs, resp, err := client.Actions.ListRepositoryWorkflowRuns(
			ctx,
			Owner,
			RepoName,
			&github.ListWorkflowRunsOptions{
				Status:     "completed",
				Created:    ">" + since.Format("2006-01-02"),
				ListOptions: *opts,
			},
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao listar workflow runs: %v\n", err)
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
	return []int64{}, avg // você pode coletar as durações individuais se quiser
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
			fmt.Fprintf(os.Stderr, "Erro ao listar PRs: %v\n", err)
			break
		}

		for _, pr := range prs {
			if pr.ClosedAt == nil || pr.ClosedAt.Before(since) {
				continue
			}

			commits, _, err := client.PullRequests.ListCommits(ctx, Owner, RepoName, *pr.Number, nil)
			if err != nil {
				continue
			}

			hasConflict := false
			for _, c := range commits {
				if c.Commit != nil && c.Commit.Message != nil {
					msg := strings.ToLower(*c.Commit.Message)
					if strings.Contains(msg, "conflict") || strings.Contains(msg, "resolve conflict") {
						hasConflict = true
						break
					}
				}
			}

			if hasConflict {
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
			fmt.Fprintf(os.Stderr, "Erro ao listar issues: %v\n", err)
			break
		}

		for _, issue := range issues {
			if issue.ClosedAt == nil || issue.ClosedAt.Before(since) {
				continue
			}

			titleLower := strings.ToLower(*issue.Title)
			hasKeyword := strings.Contains(titleLower, "rollback") || strings.Contains(titleLower, "revert")

			hasLabel := false
			for _, label := range issue.Labels {
				if strings.ToLower(*label.Name) == "rollback" {
					hasLabel = true
					break
				}
			}

			if hasKeyword || hasLabel {
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