package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ryancheng/telegram-downloader/tools/migration"
)

func main() {
	dsn := flag.String("dsn", "", "postgres dsn")
	batchSize := flag.Int("batch-size", 200, "migration batch size")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "missing required flag: -dsn")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("create postgres pool: %v", err)
	}
	defer pool.Close()

	migrator := migration.Migrator{
		Reader:    migration.NewPostgresLegacyReader(pool),
		Writer:    migration.NewPostgresV2Writer(pool),
		BatchSize: *batchSize,
	}

	stats, err := migrator.Run(ctx)
	if err != nil {
		log.Fatalf("run migration: %v", err)
	}

	fmt.Printf(
		"migration completed legacy_total=%d migrated_tasks=%d v2_tasks_total=%d legacy_tasks_checksum=%s v2_tasks_checksum=%s legacy_migrated_events_total=%d v2_migrated_events_total=%d legacy_migrated_events_checksum=%s v2_migrated_events_checksum=%s\n",
		stats.LegacyTotal,
		stats.MigratedTasks,
		stats.V2TasksTotal,
		stats.LegacyTasksChecksum,
		stats.V2TasksChecksum,
		stats.LegacyMigratedEventsTotal,
		stats.V2MigratedEventsTotal,
		stats.LegacyMigratedEventsChecksum,
		stats.V2MigratedEventsChecksum,
	)
}
