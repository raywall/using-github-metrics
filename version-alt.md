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
	Token         = "SEU_TOKEN"                // Personal Access Token com escopos: repo, workflow
	Owner         = "owner"                    // ex: "minha-org"
	RepoName      = "repo"                     // ex: "meu-projeto"
	WorkflowName  = "2 - [DEV] Build & Deploy" // Nome exato do workflow
	Branch        = "main"                     // Branch para tamanho
	MonthsBack    = 1                          // Quantos meses para trás
)

func main() {
	ctx := context.Background()

	// Autenticação
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: Token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Tamanho da branch
	branchSize, err := getBranchSize(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao calcular tamanho da branch: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Repositório: %s/%s\n", Owner, RepoName)
	fmt.Printf("Tamanho da branch '%s': %d KB\n", Branch, branchSize/1024)

	oneMonthAgo := time.Now().AddDate(0, -MonthsBack, 0)
	now := time.Now()
	fmt.Printf("Período analisado: %s a %s\n\n", oneMonthAgo.Format(time.RFC3339), now.Format(time.RFC3339))

	// 1. Workflow duração média
	fmt.Println("Analisando workflow durations...")
	_, avgDuration := getWorkflowDurations(ctx, client, oneMonthAgo)

	// 2. Média de re-runs / attempts
	fmt.Println("Analisando número médio de tentativas (re-runs)...")
	avgAttempts, totalRuns := getAverageRunAttempts(ctx, client, oneMonthAgo)
	fmt.Printf("Média de tentativas por run (incluindo re-runs): %.2f (baseado em %d runs completados)\n\n", avgAttempts, totalRuns)

	// 3. Conflitos de merge
	fmt.Println("Analisando conflitos de merge...")
	conflictCount := countMergeConflicts(ctx, client, oneMonthAgo)
	fmt.Printf("Quantidade de merges com conflitos resolvidos no período: %d\n\n", conflictCount)

	// 4. Issues de rollback
	fmt.Println("Analisando issues de rollback...")
	rollbackCount := countRollbackIssues(ctx, client, oneMonthAgo)
	fmt.Printf("Quantidade de issues de rollback: %d\n\n", rollbackCount)

	// Resumo final
	fmt.Println("Resumo:")
	fmt.Printf("- Tamanho branch '%s': %d KB\n", Branch, branchSize/1024)
	fmt.Printf("- Duração média workflow '%s': %.2f s\n", WorkflowName, avgDuration)
	fmt.Printf("- Média de tentativas por run '%s': %.2f (total runs: %d)\n", WorkflowName, avgAttempts, totalRuns)
	fmt.Printf("- Merges com conflitos resolvidos: %d\n", conflictCount)
	fmt.Printf("- Issues de rollback: %d\n", rollbackCount)
}

// ... (mantenha as funções getBranchSize, getWorkflowDurations, countMergeConflicts e countRollbackIssues como estavam)

// Nova função: calcula média de tentativas (attempts) por workflow run
func getAverageRunAttempts(ctx context.Context, client *github.Client, since time.Time) (float64, int) {
	// Encontrar o ID do workflow
	workflows, _, err := client.Actions.ListWorkflows(ctx, Owner, RepoName, &github.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao listar workflows: %v\n", err)
		return 0, 0
	}

	var workflowID int64
	for _, wf := range workflows.Workflows {
		if wf.Name != nil && *wf.Name == WorkflowName {
			workflowID = *wf.ID
			break
		}
	}
	if workflowID == 0 {
		fmt.Fprintf(os.Stderr, "Workflow '%s' não encontrado\n", WorkflowName)
		return 0, 0
	}

	var totalAttempts int
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
			fmt.Fprintf(os.Stderr, "Erro ao listar runs: %v\n", err)
			break
		}

		for _, run := range runs.WorkflowRuns {
			if run.Conclusion != nil && *run.Conclusion == "success" { // ou remova isso se quiser todos completed
				attempt := 1 // default
				if run.RunAttempt != nil {
					attempt = *run.RunAttempt
				}
				totalAttempts += attempt
				count++
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if count == 0 {
		return 0, 0
	}

	avg := float64(totalAttempts) / float64(count)
	return avg, count
}