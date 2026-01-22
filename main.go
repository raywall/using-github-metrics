package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/raywall/using-gh-metrics/analyzer"
)

var svc *analyzer.Analyzer

func init() {
	svc = analyzer.NewAnalyzer(
		"raywall",
		"main",
		"2 - [DEV] Build & Deploy",
		time.Now().AddDate(0, -13, 0).Truncate(24*time.Hour),
		time.Now().AddDate(0, -1, 0).Truncate(24*time.Hour),
		os.Getenv("GITHUB_TOKEN"),
		map[string][]string{
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
		},
	)
}

func main() {
	ctx := context.Background()
	metrics, err := svc.Check(ctx)
	if err != nil {
		log.Println("falha ao recuperar métricas do GitHub")
	}
	svc.Export(metrics, "output.json")
}
