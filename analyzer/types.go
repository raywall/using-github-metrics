package analyzer

import (
	"time"

	"github.com/google/go-github/v62/github"
)

// RepoMetrics holds all the computed metrics for a single repository.
type RepoMetrics struct {
	Repo                string         `json:"repo"`
	UniqueContributors  int            `json:"unique_contributors"`
	ContributorsList    []string       `json:"contributors_list"`
	CommitDist          map[string]int `json:"commit_dist"`
	ConflictRate        float64        `json:"conflict_rate"`
	AvgMergeTimeDays    float64        `json:"avg_merge_time_days"`
	AvgReviewersPerPR   float64        `json:"avg_reviewers_per_pr"`
	CrossTeamReviews    int            `json:"cross_team_reviews"`
	ChurnByFile         map[string]int `json:"churn_by_file"`
	ChurnByDir          map[string]int `json:"churn_by_dir"`
	IntegrationIssues   int            `json:"integration_issues"`
	RevertRate          float64        `json:"revert_rate"`
	MainBranchSizeBytes int64          `json:"main_branch_size_bytes"`
	MainFileCount       int            `json:"main_file_count"`
	SuccessfulReruns    int            `json:"successful_reruns"`
	ConflictMergesCount int            `json:"conflict_merges_count"`
	RollbackIssues      int            `json:"rollback_issues"`
	WorkflowFailures    int            `json:"workflow_failures"`
	SuccessfulDeploys   int            `json:"successful_deploys"`
	AvgThreadDepth      float64        `json:"avg_thread_depth"`
}

// Analyzer is the main struct for GitHub metrics analysis.
type Analyzer struct {
	Owner         string
	DefaultBranch string
	WorkflowID    string // Can be name or ID; we'll assume string ID and parse to int64
	StartDate     time.Time
	EndDate       time.Time
	Token         string
	Projects      map[string][]string // Key: area/product, Value: []repos
	client        *github.Client
}
