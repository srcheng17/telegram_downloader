package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

const advisoryKey int64 = 836415019204

type Coordinator struct {
	pool *pgxpool.Pool
	root string
}

func NewCoordinator(pool *pgxpool.Pool, root string) (*Coordinator, error) {
	if pool == nil || !filepath.IsAbs(root) {
		return nil, errors.New("telegram configuration invalid")
	}
	if err := privateDirectory(root); err != nil {
		return nil, err
	}
	return &Coordinator{pool: pool, root: root}, nil
}
func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil || actual != filepath.Clean(path) {
		return errors.New("unsafe telegram directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("telegram directory requires mode 0700")
	}
	return nil
}
func lockFile(root string) (*os.File, error) {
	fd, err := syscall.Open(filepath.Join(root, ".account.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "telegram-account-lock")
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("unsafe telegram lock")
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, app.ErrBusy
		}
		return nil, err
	}
	return f, nil
}
func (c *Coordinator) Busy(ctx context.Context) (bool, error) {
	f, err := lockFile(c.root)
	if errors.Is(err, app.ErrBusy) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	f.Close()
	return false, nil
}
func (c *Coordinator) Do(ctx context.Context, wait bool, fn func(context.Context, app.Lease) error) error {
	for {
		err := c.once(ctx, fn)
		if !wait || !errors.Is(err, app.ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (c *Coordinator) once(ctx context.Context, fn func(context.Context, app.Lease) error) error {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return app.ErrBusy
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := conn.Exec(release, `SELECT pg_advisory_unlock($1)`, advisoryKey); err != nil {
			_ = conn.Conn().Close(release)
		}
	}()
	f, err := lockFile(c.root)
	if err != nil {
		return err
	}
	defer f.Close()
	active, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	stopped := make(chan struct{})
	lost := make(chan struct{}, 1)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(active, time.Second)
				err := conn.Ping(probe)
				stop()
				if err != nil {
					lost <- struct{}{}
					cancel()
					return
				}
			}
		}
	}()
	err = fn(active, app.Lease{File: f})
	close(done)
	<-stopped
	select {
	case <-lost:
		return app.Failure("coordination_lost")
	default:
		return err
	}
}
