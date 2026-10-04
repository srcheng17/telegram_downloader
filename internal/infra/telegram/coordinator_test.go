package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

func coordinatorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	if e = pool.Ping(ctx); e != nil {
		pool.Close()
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	return pool
}
func TestCoordinatorProcess(t *testing.T) {
	root := os.Getenv("TELEGRAM_LOCK_TEST_ROOT")
	if root == "" {
		return
	}
	pool := coordinatorPool(t)
	c, e := NewCoordinator(pool, root)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	e = c.Do(ctx, false, func(ctx context.Context, _ app.Lease) error { fmt.Println("held"); <-ctx.Done(); return ctx.Err() })
	if errors.Is(e, app.ErrBusy) {
		fmt.Println("busy")
		return
	}
	if e != nil && !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestCoordinatorThreeProcessesContendForOneAccount(t *testing.T) {
	coordinatorPool(t)
	root := privateTestRoot(t)
	launch := func() (*exec.Cmd, *bufio.Scanner) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorProcess$")
		cmd.Env = append(os.Environ(), "TELEGRAM_LOCK_TEST_ROOT="+root)
		pipe, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd, bufio.NewScanner(pipe)
	}
	first, lines := launch()
	if !lines.Scan() || lines.Text() != "held" {
		t.Fatal("first process failed to acquire")
	}
	for i := 0; i < 2; i++ {
		cmd, output := launch()
		if !output.Scan() || output.Text() != "busy" {
			t.Fatal("competing process entered account")
		}
		if e := cmd.Wait(); e != nil {
			t.Fatal(e)
		}
	}
	if e := first.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = first.Wait()
	pool := coordinatorPool(t)
	c, e := NewCoordinator(pool, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Do(context.Background(), false, func(context.Context, app.Lease) error { return nil }); e != nil {
		t.Fatal("dead process left lock held", e)
	}
}
func TestCoordinatorPostgresLossCancelsAndReapsChild(t *testing.T) {
	pool := coordinatorPool(t)
	b, owned := bridgeFixture(t, "exec sleep 30")
	owned.File.Close()
	c, e := NewCoordinator(pool, b.options.PrivateRoot)
	if e != nil {
		t.Fatal(e)
	}
	started := make(chan struct{})
	result := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	go func() {
		result <- c.Do(ctx, false, func(active context.Context, lease app.Lease) error {
			p, e := b.Login(active, lease, "candidate_"+strings.Repeat("a", 32))
			if e != nil {
				return e
			}
			close(started)
			for range p.Events() {
			}
			return p.Wait()
		})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("helper not started")
	}
	var pid int32
	if e = pool.QueryRow(ctx, `SELECT pid FROM pg_locks WHERE locktype='advisory' AND granted AND classid=$1 AND objid=$2`, uint32(advisoryKey>>32), uint32(advisoryKey&0xffffffff)).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	var killed bool
	if e = pool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&killed); e != nil || !killed {
		t.Fatal("test could not terminate dedicated connection", e)
	}
	select {
	case e = <-result:
		if errorCodeFixture(e) != "coordination_lost" {
			t.Fatal("lost coordination not reported", e)
		}
	case <-ctx.Done():
		t.Fatal("lost lock did not cancel child")
	}
	if busy, e := c.Busy(ctx); e != nil || busy {
		t.Fatal("child or parent retained lock after wait")
	}
	if e = c.Do(ctx, false, func(context.Context, app.Lease) error { return nil }); e != nil {
		t.Fatal("coordinator did not recover", e)
	}
}
