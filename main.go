package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

var (
	token = os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN não definido")
	}

	// global settings
	Owner       = "raywall" // your org or username
	Branch      = "main"
	WorkflowName = "2 - [DEV] Build & Deploy"
	MonthsBack  = 1 // adjust as needed

	// repositories by area
	ReposByArea = map[string][]string{
		"Backend": {
			"fast-service-toolkit",
			"payment-service",
			"auth-service",
		},
		"Frontend": {
			"web-app",
			"admin-panel",
		},
		"Infrastructure": {
			"terraform-modules",
			"monitoring-stack",
		},
		// add more areas/repos as needed
	}

	// limit on simultaneous goroutines (avoids GitHub's rate limit)
	maxConcurrency = 5
)

type RepoResult struct {
	Area          string
	Repo          string
	BranchSizeMB  float64
	PeriodFrom    string
	PeriodTo      string
	AvgAttempts   float64
	TotalRuns     int
	TotalAttempts int // sum of all attempts
	Conflicts     int
	Rollbacks     int
	Err           error
}

func main() {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	logChan := make(chan string, 100)
	wgLog := sync.WaitGroup{}
	wgLog.Add(1)
	go func() {
		defer wgLog.Done()
		f, err := os.OpenFile("github-analysis.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("erro ao criar log file: %v", err)
			return
		}
		defer f.Close()

		for msg := range logChan {
			fmt.Fprintln(f, msg)
			fmt.Println(msg) // also on the console
		}
	}()

	results := make([]RepoResult, 0)
	resultsMu := sync.Mutex{}
	sem := make(chan struct{}, maxConcurrency)
	wg := sync.WaitGroup{}

	from := time.Now().AddDate(0, -(MonthsBack+1), 0).Truncate(24 * time.Hour)
	to := time.Now().AddDate(0, -1, 0).Truncate(24 * time.Hour)

	logChan <- fmt.Sprintf("Período global: %s a %s", from.Format("02-01-2006"), to.Format("02-01-2006"))

	for area, repos := range ReposByArea {
		for _, repo := range repos {
			wg.Add(1)
			sem <- struct{}{}
			go func(area, repo string) {
				defer wg.Done()
				defer func() { <-sem }()

				logChan <- fmt.Sprintf("Processando %s/%s...", area, repo)

				res := processRepo(ctx, client, area, repo, from, to)
				resultsMu.Lock()
				results = append(results, res)
				resultsMu.Unlock()

				if res.Err != nil {
					logChan <- fmt.Sprintf("Erro em %s/%s: %v", area, repo, res.Err)
				}
			}(area, repo)
		}
	}

	wg.Wait()
	close(logChan)
	wgLog.Wait()

	generateMarkdown(results, from, to)
	log.Println("Análise concluída. Arquivo gerado: output.md")
}

func processRepo(ctx context.Context, client *github.Client, area, repo string, from, to time.Time) RepoResult {
	res := RepoResult{
		Area:       area,
		Repo:       repo,
		PeriodFrom: from.Format("02-01-2006"),
		PeriodTo:   to.Format("02-01-2006"),
	}

	// branch size
	sizeBytes, err := getBranchSize(ctx, client, repo)
	if err != nil {
		res.Err = err
		return res
	}
	res.BranchSizeMB = float64(sizeBytes) / (1024 * 1024)

	// workflow ID (simple cache per repo)
	workflowID, err := getWorkflowID(ctx, client, repo)
	if err != nil {
		res.Err = err
		return res
	}

	// media and total attempts
	avgAtt, totalRuns, totalAtt, err := getAverageAndTotalRunAttempts(ctx, client, repo, workflowID, from)
	if err != nil {
		res.Err = err
		return res
	}
	res.AvgAttempts = avgAtt
	res.TotalRuns = totalRuns
	res.TotalAttempts = totalAtt

	// conflicts (using mergeable_state == "dirty")
	res.Conflicts, err = countMergeConflicts(ctx, client, repo, from)
	if err != nil {
		res.Err = err
		return res
	}

	// rollback issues
	res.Rollbacks, err = countRollbackIssues(ctx, client, repo, from)
	if err != nil {
		res.Err = err
		return res
	}

	return res
}

func getBranchSize(ctx context.Context, client *github.Client, repo string) (int64, error) {
	ref, _, err := client.Git.GetRef(ctx, Owner, repo, "heads/"+Branch)
	if err != nil {
		return 0, err
	}
	tree, _, err := client.Git.GetTree(ctx, Owner, repo, *ref.Object.SHA, true)
	if err != nil {
		return 0, err
	}
	var size int64
	for _, e := range tree.Entries {
		if *e.Type == "blob" && e.Size != nil {
			size += int64(*e.Size)
		}
	}
	return size, nil
}

func getWorkflowID(ctx context.Context, client *github.Client, repo string) (int64, error) {
	workflows, _, err := client.Actions.ListWorkflows(ctx, Owner, repo, &github.ListOptions{PerPage: 100})
	if err != nil {
		return 0, err
	}
	for _, wf := range workflows.Workflows {
		if wf.Name != nil && *wf.Name == WorkflowName {
			return *wf.ID, nil
		}
	}
	return 0, fmt.Errorf("workflow %q não encontrado em %s", WorkflowName, repo)
}

func getAverageAndTotalRunAttempts(ctx context.Context, client *github.Client, repo string, workflowID int64, since time.Time) (avg float64, totalRuns, totalAttempts int) {
	opts := &github.ListOptions{PerPage: 100}
	for {
		runs, resp, err := client.Actions.ListWorkflowRunsByID(ctx, Owner, repo, workflowID,
			&github.ListWorkflowRunsOptions{
				Status:      "completed",
				Created:     ">" + since.Format("2006-01-02"),
				ListOptions: *opts,
			})
		if err != nil {
			return 0, 0, 0
		}
		for _, r := range runs.WorkflowRuns {
			if r.Conclusion != nil && *r.Conclusion == "success" {
				attempt := 1
				if r.RunAttempt != nil {
					attempt = *r.RunAttempt
				}
				totalAttempts += attempt
				totalRuns++
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	if totalRuns == 0 {
		return 0, 0, 0
	}
	return float64(totalAttempts) / float64(totalRuns), totalRuns, totalAttempts
}

func countMergeConflicts(ctx context.Context, client *github.Client, repo string, since time.Time) (int, error) {
	count := 0
	opts := &github.PullRequestListOptions{
		State:       "closed",
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 50}, // smaller for performance
	}
	for {
		prs, resp, err := client.PullRequests.List(ctx, Owner, repo, opts)
		if err != nil {
			return 0, err
		}
		for _, pr := range prs {
			if pr.ClosedAt != nil && !pr.ClosedAt.Before(since) {
				full, _, err := client.PullRequests.Get(ctx, Owner, repo, *pr.Number)
				if err == nil && full.Mergeable != nil && !*full.Mergeable &&
					full.MergeableState != nil && *full.MergeableState == "dirty" {
					count++
				}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return count, nil
}

func countRollbackIssues(ctx context.Context, client *github.Client, repo string, since time.Time) (int, error) {
	count := 0
	opts := &github.IssueListByRepoOptions{
		State:       "closed",
		Since:       since,
		ListOptions: github.ListOptions{PerPage: 50},
	}
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, Owner, repo, opts)
		if err != nil {
			return 0, err
		}
		for _, issue := range issues {
			if issue.ClosedAt != nil && !issue.ClosedAt.Before(since) {
				for _, lbl := range issue.Labels {
					if lbl.Name != nil && strings.Contains(strings.ToLower(*lbl.Name), "rollback") {
						count++
						break
					}
				}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return count, nil
}

func generateMarkdown(results []RepoResult, from, to time.Time) {
	f, err := os.Create("output.md")
	if err != nil {
		log.Fatalf("erro ao criar output.md: %v", err)
	}
	defer f.Close()

	// group by area and sort
	areas := make([]string, 0, len(ReposByArea))
	for a := range ReposByArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)

	for _, area := range areas {
		fmt.Fprintf(f, "### %s\n\n", area)

		// filter area results
		var areaResults []RepoResult
		for _, r := range results {
			if r.Area == area {
				areaResults = append(areaResults, r)
			}
		}
		// sort by repo name
		sort.Slice(areaResults, func(i, j int) bool {
			return areaResults[i].Repo < areaResults[j].Repo
		})

		for _, r := range areaResults {
			if r.Err != nil {
				fmt.Fprintf(f, "**Repositório**: %s — **ERRO**: %v\n\n", r.Repo, r.Err)
				continue
			}

			repoLink := fmt.Sprintf("https://github.com/%s/%s", Owner, r.Repo)
			reRuns := r.TotalAttempts - r.TotalRuns // effective amount of reruns

			fmt.Fprintf(f, "**Repositório**: [%s](%s)\n", r.Repo, repoLink)
			fmt.Fprintf(f, "**Tamanho da branch `%s`**: %.2f MB\n", Branch, r.BranchSizeMB)
			fmt.Fprintf(f, "**Período avaliado**: %s a %s\n", r.PeriodFrom, r.PeriodTo)
			fmt.Fprintf(f, "**Quantidade média de re-tentativas (re-runs) do workflow**: %.2f\n", r.AvgAttempts)
			fmt.Fprintf(f, "**Quantidade total de re-tentativas (re-runs) do workflow**: %d\n", reRuns)
			fmt.Fprintf(f, "**Quantidade de merges com conflitos resolvidos no período**: %d\n", r.Conflicts)
			fmt.Fprintf(f, "**Quantidade de issues de rollback no período**: %d\n\n", r.Rollbacks)
		}
	}
}