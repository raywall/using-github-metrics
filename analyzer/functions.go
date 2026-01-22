package analyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/go-github/v62/github"
)

// checkRateLimit checks the rate limit and sleeps if necessary.
func (a *Analyzer) checkRateLimit(resp *github.Response) {
	if resp.Rate.Remaining < 100 {
		time.Sleep(5 * time.Second) // Adjustable
	}
}

// getUsernames converts a map to a slice of usernames.
func getUsernames(unique map[string]struct{}) []string {
	var list []string
	for u := range unique {
		list = append(list, u)
	}
	return list
}

// FormatSecondsToHMS transforms seconds into hh:mm:ss format.
func (a *Analyzer) FormatSecondsToHMS(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// Export exports the metrics to a JSON file.
func (a *Analyzer) Export(metrics []RepoMetrics, filename string) error {
	jsonData, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, jsonData, 0644)
}
