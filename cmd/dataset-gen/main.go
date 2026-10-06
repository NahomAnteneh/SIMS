package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/infrastructure/sims/internal/benchmark"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	vmCount := flag.Int("vms", 10000, "Number of active VMs to generate (e.g. 10000, 100000, 500000, 1000000)")
	vcCount := flag.Int("vcenters", 1, "Number of vCenters to distribute across")
	batchSize := flag.Int("batch", 5000, "Batch size for binary COPY streaming")
	cleanFirst := flag.Bool("clean", false, "Clean existing test data before seeding")
	runWorkload := flag.Bool("workload", false, "Execute query workload benchmark after dataset generation")
	workloadDuration := flag.Duration("duration", 5*time.Second, "Workload benchmark duration")
	concurrency := flag.Int("concurrency", 10, "Workload benchmark concurrency")

	defaultDBURL := os.Getenv("DATABASE_URL")
	if defaultDBURL == "" {
		host := os.Getenv("POSTGRES_HOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("POSTGRES_PORT")
		if port == "" {
			port = "5433"
		}
		defaultDBURL = fmt.Sprintf("postgres://postgres:postgres@%s:%s/sims?sslmode=disable", host, port)
	}
	dbURL := flag.String("db", defaultDBURL, "PostgreSQL connection URL")
	flag.Parse()

	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(*dbURL)
	if err != nil {
		log.Fatalf("Invalid database connection string: %v", err)
	}
	poolConfig.MaxConns = 30
	poolConfig.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Database ping failed: %v", err)
	}

	fmt.Printf("=== SIMS Synthetic Performance Dataset Generator ===\n")
	fmt.Printf("Target VM Count:       %d\n", *vmCount)
	fmt.Printf("vCenter Fleet Count:   %d\n", *vcCount)
	fmt.Printf("Batch Size:            %d\n", *batchSize)
	fmt.Printf("Database URL:          %s\n", *dbURL)
	fmt.Printf("Clean First:           %v\n", *cleanFirst)
	fmt.Println("-----------------------------------------------------")

	cfg := benchmark.DatasetConfig{
		VMCount:      *vmCount,
		VCenterCount: *vcCount,
		BatchSize:    *batchSize,
		CleanFirst:   *cleanFirst,
		OnProgress: func(completedVMs int, elapsed time.Duration) {
			pct := float64(completedVMs) / float64(*vmCount) * 100
			vmsPerSec := float64(completedVMs) / elapsed.Seconds()
			fmt.Printf("[Progress] %d / %d VMs (%.1f%%) in %s (%.0f VMs/sec)\n",
				completedVMs, *vmCount, pct, elapsed.Round(100*time.Millisecond), vmsPerSec)
		},
	}

	generator := benchmark.NewGenerator(pool, cfg)
	stats, err := generator.Generate(ctx)
	if err != nil {
		log.Fatalf("Dataset generation failed: %v", err)
	}

	totalChildRows := stats.NICCount + stats.DatastoreAttachCount + stats.TagCount + stats.CustomAttrCount
	fmt.Println("-----------------------------------------------------")
	fmt.Printf("Dataset Generation Completed Successfully!\n")
	fmt.Printf("Elapsed Time:          %s\n", stats.Duration.Round(10*time.Millisecond))
	fmt.Printf("Throughput:            %.1f VMs/sec\n", stats.ThroughputVMsPerSec)
	fmt.Printf("Active VMs:            %d\n", stats.VMCount)
	fmt.Printf("Network Interfaces:    %d\n", stats.NICCount)
	fmt.Printf("Datastore Attachments: %d\n", stats.DatastoreAttachCount)
	fmt.Printf("Tags Attached:         %d\n", stats.TagCount)
	fmt.Printf("Custom Attributes:     %d\n", stats.CustomAttrCount)
	fmt.Printf("Total Child Rows:      %d\n", totalChildRows)
	fmt.Println("-----------------------------------------------------")

	if *runWorkload {
		fmt.Printf("\nExecuting Mixed Query Workload Model (%s, %d workers)...\n", *workloadDuration, *concurrency)
		workloadCfg := benchmark.WorkloadConfig{
			Concurrency: *concurrency,
			Duration:    *workloadDuration,
		}
		runner := benchmark.NewWorkloadRunner(pool, workloadCfg, stats)
		res, err := runner.Run(ctx)
		if err != nil {
			log.Fatalf("Workload benchmark failed: %v", err)
		}

		fmt.Println("=== Query Workload Benchmark Results ===")
		fmt.Printf("Total Queries: %d (Errors: %d)\n", res.TotalQueries, res.ErrorCount)
		fmt.Printf("Throughput:    %.1f QPS\n", res.QPS)
		fmt.Printf("P50 Latency:   %s\n", res.P50)
		fmt.Printf("P90 Latency:   %s\n", res.P90)
		fmt.Printf("P95 Latency:   %s\n", res.P95)
		fmt.Printf("P99 Latency:   %s\n", res.P99)
		fmt.Printf("Min:           %s\n", res.Min)
		fmt.Printf("Max:           %s\n", res.Max)
		fmt.Printf("Mean:          %s\n", res.Mean)
		fmt.Println("=========================================")
	}
}
