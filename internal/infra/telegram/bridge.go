package telegram

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

type BridgeOptions struct{ HelperPath, TDLPath, PrivateRoot string }
type Bridge struct{ options BridgeOptions }

func NewBridge(o BridgeOptions) (*Bridge, error) {
	if !filepath.IsAbs(o.HelperPath) || !filepath.IsAbs(o.TDLPath) || !filepath.IsAbs(o.PrivateRoot) {
		return nil, errors.New("telegram executable paths must be absolute")
	}
	for _, path := range []string{o.HelperPath, o.TDLPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return nil, errors.New("telegram executable unavailable")
		}
	}
	if err := privateDirectory(o.PrivateRoot); err != nil {
		return nil, err
	}
	if err := privateDirectory(filepath.Join(o.PrivateRoot, "storage")); err != nil {
		return nil, err
	}
	return &Bridge{options: o}, nil
}

type bridgeCommand struct {
	Mode        string     `json:"mode"`
	StoragePath string     `json:"storage_path"`
	Namespace   string     `json:"namespace"`
	MessageURL  string     `json:"message_url,omitempty"`
	Password    string     `json:"password,omitempty"`
	TDLPath     string     `json:"tdl_path,omitempty"`
	OutputDir   string     `json:"output_dir,omitempty"`
	MaxBytes    int64      `json:"max_bytes,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}
type process struct {
	input           io.WriteCloser
	events          chan app.BridgeEvent
	done            chan struct{}
	mu              sync.Mutex
	err             error
	passwordAllowed bool
}

func (p *process) Events() <-chan app.BridgeEvent { return p.events }
func (p *process) Wait() error                    { <-p.done; return p.err }
func (p *process) Password(password string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.passwordAllowed || len(password) < 1 || len(password) > 1024 {
		return app.ErrConflict
	}
	p.passwordAllowed = false
	return json.NewEncoder(p.input).Encode(bridgeCommand{Mode: "password", Password: password})
}
func knownEvent(e app.BridgeEvent) bool {
	switch e.Type {
	case "qr":
		return strings.HasPrefix(e.QR, "data:image/png;base64,") && len(e.QR) <= 24000 && e.ExpiresAt != nil && e.ExpiresAt.After(time.Now()) && e.ExpiresAt.Before(time.Now().Add(10*time.Minute))
	case "authorized":
		return len(e.Identity) == 64
	case "attachment":
		return len(e.Identity) == 64 && e.Size > 0 && (e.Extension == ".zip" || e.Extension == ".rar" || e.Extension == ".7z")
	case "password_required", "password_invalid":
		return e.QR == "" && e.Identity == ""
	case "error":
		switch e.Code {
		case "authorization_unconfirmed", "password_invalid", "rate_limited", "auth_required", "cancelled", "expired", "no_attachment", "unsupported_media", "ambiguous_media", "source_too_large", "source_size_unknown", "invalid_input", "network_error":
			return true
		}
	}
	return false
}
func (b *Bridge) start(ctx context.Context, lease app.Lease, c bridgeCommand, raw bool) (*process, error) {
	if lease.File == nil {
		return nil, app.Failure("coordination_lost")
	}
	c.StoragePath = filepath.Join(b.options.PrivateRoot, "storage")
	if !raw {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(45 * time.Second)
			if c.Mode == "login" {
				deadline = time.Now().Add(8 * time.Minute)
			}
		}
		c.ExpiresAt = &deadline
	}
	active, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(active, b.options.HelperPath)
	cmd.Env = []string{"HOME=" + b.options.PrivateRoot, "PATH=/usr/local/bin:/usr/bin:/bin", "NO_COLOR=1"}
	cmd.ExtraFiles = []*os.File{lease.File}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Context cancellation kills the whole process group, then Wait drains/reaps.
	// No raw stderr/progress reaches API logs, HTTP bodies or the event protocol.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	var stdout io.ReadCloser
	if raw {
		cmd.Stdout = io.Discard
	} else {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			cancel()
			return nil, err
		}
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		if stdout != nil {
			stdout.Close()
		}
		cancel()
		return nil, err
	}
	p := &process{input: stdin, events: make(chan app.BridgeEvent, 8), done: make(chan struct{})}
	if err = json.NewEncoder(stdin).Encode(c); err != nil {
		cancel()
		_ = cmd.Wait()
		stdin.Close()
		return nil, app.Failure("bridge_unavailable")
	}
	go func() {
		defer close(p.done)
		defer close(p.events)
		defer cancel()
		defer stdin.Close()
		var protocolErr error
		if !raw {
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 4096), 32768)
			count := 0
			window := time.Now()
			for scanner.Scan() {
				if time.Since(window) > time.Second {
					count = 0
					window = time.Now()
				}
				count++
				if count > 16 {
					protocolErr = app.Failure("invalid_bridge_output")
					cancel()
					break
				}
				var e app.BridgeEvent
				decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&e) != nil || decoder.Decode(new(any)) != io.EOF || !knownEvent(e) {
					protocolErr = app.Failure("invalid_bridge_output")
					cancel()
					break
				}
				if e.Type == "password_required" {
					p.mu.Lock()
					p.passwordAllowed = true
					p.mu.Unlock()
				}
				select {
				case p.events <- e:
				case <-active.Done():
					protocolErr = active.Err()
				}
				if protocolErr != nil {
					break
				}
			}
			if scanner.Err() != nil && protocolErr == nil {
				protocolErr = app.Failure("invalid_bridge_output")
				cancel()
			}
		}
		err := cmd.Wait()
		if ctx.Err() != nil {
			p.err = ctx.Err()
		} else if protocolErr != nil {
			p.err = protocolErr
		} else if err != nil {
			p.err = app.Failure("bridge_failed")
		}
	}()
	return p, nil
}
func (b *Bridge) Login(ctx context.Context, l app.Lease, namespace string) (app.LoginProcess, error) {
	return b.start(ctx, l, bridgeCommand{Mode: "login", Namespace: namespace}, false)
}
func (b *Bridge) single(ctx context.Context, l app.Lease, c bridgeCommand, wanted string) (app.BridgeEvent, error) {
	p, err := b.start(ctx, l, c, false)
	if err != nil {
		return app.BridgeEvent{}, err
	}
	var result app.BridgeEvent
	var failure error
	for event := range p.Events() {
		if event.Type == wanted {
			result = event
		}
		if event.Type == "error" {
			failure = app.Failure(event.Code)
		}
	}
	err = p.Wait()
	if failure != nil {
		return result, failure
	}
	if err != nil {
		return result, err
	}
	if result.Type != wanted {
		return result, app.Failure("authorization_unconfirmed")
	}
	return result, nil
}
func (b *Bridge) Verify(ctx context.Context, l app.Lease, namespace string) (string, error) {
	e, err := b.single(ctx, l, bridgeCommand{Mode: "verify", Namespace: namespace}, "authorized")
	return e.Identity, err
}
func (b *Bridge) Inspect(ctx context.Context, l app.Lease, namespace, messageURL string, max int64) (app.BridgeEvent, error) {
	return b.single(ctx, l, bridgeCommand{Mode: "inspect", Namespace: namespace, MessageURL: messageURL, MaxBytes: max}, "attachment")
}
func (b *Bridge) Download(ctx context.Context, l app.Lease, namespace, messageURL, outputDir string, max int64) error {
	p, err := b.start(ctx, l, bridgeCommand{Mode: "download", Namespace: namespace, MessageURL: messageURL, TDLPath: b.options.TDLPath, OutputDir: outputDir, MaxBytes: max}, true)
	if err != nil {
		return err
	}
	return p.Wait()
}

func (b *Bridge) Discard(_ context.Context, lease app.Lease, namespace string) error {
	if lease.File == nil || !regexp.MustCompile(`^candidate_[a-f0-9]{32}$`).MatchString(namespace) {
		return app.Failure("invalid_input")
	}
	err := os.Remove(filepath.Join(b.options.PrivateRoot, "storage", namespace))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
