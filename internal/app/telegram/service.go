package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Options struct {
	AttemptTTL     time.Duration
	VerifyTimeout  time.Duration
	MaxSourceBytes int64
}
type Service struct {
	store            Store
	coordinator      Coordinator
	bridge           Bridge
	options          Options
	ctx              context.Context
	stop             context.CancelFunc
	mu               sync.Mutex
	workers          sync.WaitGroup
	closed           bool
	current          *attempt
	checkedRevision  int64
	verificationCode string
	verifiedRevision int64
	verifiedAt       time.Time
}
type attempt struct {
	record  AttemptRecord
	process LoginProcess
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewService(store Store, coordinator Coordinator, bridge Bridge, options Options) (*Service, error) {
	if store == nil || coordinator == nil || bridge == nil {
		return nil, Failure("unavailable")
	}
	if options.AttemptTTL == 0 {
		options.AttemptTTL = 8 * time.Minute
	}
	if options.VerifyTimeout == 0 {
		options.VerifyTimeout = 45 * time.Second
	}
	if options.MaxSourceBytes == 0 {
		options.MaxSourceBytes = MaximumSourceBytes
	}
	if options.AttemptTTL < time.Second || options.AttemptTTL > 15*time.Minute || options.VerifyTimeout <= 0 || options.MaxSourceBytes < 1 || options.MaxSourceBytes > MaximumSourceBytes {
		return nil, Failure("invalid_configuration")
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Service{store: store, coordinator: coordinator, bridge: bridge, options: options, ctx: ctx, stop: stop}, nil
}
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.stop()
	s.mu.Unlock()
	s.workers.Wait()
}
func randomID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(data[:])
}
func terminal(state string) bool {
	return state == "connected" || state == "failed" || state == "expired" || state == "cancelled"
}
func codeOf(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	if errors.Is(err, ErrBusy) {
		return "busy"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "expired"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "unavailable"
}
func publicSnapshot(record AttemptRecord) Snapshot {
	v := record.Snapshot
	if v.QRExpiresAt == nil || !v.QRExpiresAt.After(time.Now()) || terminal(v.State) {
		v.QR = ""
		v.QRExpiresAt = nil
	}
	return v
}
func (s *Service) Account(ctx context.Context) (Account, error) {
	r, err := s.store.Account(ctx)
	if err != nil {
		return Account{}, err
	}
	busy, err := s.coordinator.Busy(ctx)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	verified := s.verifiedRevision == r.Revision && r.Revision > 0
	checked := s.checkedRevision == r.Revision
	code := s.verificationCode
	at := s.verifiedAt
	s.mu.Unlock()
	state := "auth_required"
	if r.Revision > 0 && !checked && !busy {
		_ = s.VerifyAccount(ctx)
		s.mu.Lock()
		verified = s.verifiedRevision == r.Revision
		checked = s.checkedRevision == r.Revision
		code = s.verificationCode
		at = s.verifiedAt
		s.mu.Unlock()
	}
	if verified {
		state = "connected"
	} else if r.Revision > 0 && (!checked || code != "auth_required") {
		state = "unverified"
	}
	result := Account{State: state, Revision: r.Revision, Busy: busy, MaxSourceBytes: s.options.MaxSourceBytes}
	if !at.IsZero() {
		result.VerifiedAt = &at
	} else if !r.VerifiedAt.IsZero() {
		result.VerifiedAt = &r.VerifiedAt
	}
	latest, err := s.store.LatestAttempt(ctx)
	if err == nil {
		v, e := s.Attempt(ctx, latest.ID)
		if e == nil {
			result.Attempt = &v
		}
	} else if !errors.Is(err, ErrNotFound) {
		return Account{}, err
	}
	return result, nil
}
func (s *Service) VerifyAccount(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.options.VerifyTimeout)
	defer cancel()
	return s.coordinator.Do(ctx, false, func(ctx context.Context, lease Lease) error {
		r, err := s.store.Account(ctx)
		if err != nil {
			return err
		}
		if r.Revision == 0 {
			return Failure("auth_required")
		}
		identity, err := s.bridge.Verify(ctx, lease, r.Namespace)
		if err == nil && identity != r.Identity {
			err = Failure("account_changed")
		}
		s.mu.Lock()
		s.checkedRevision = r.Revision
		s.verificationCode = ""
		s.verifiedRevision = 0
		if err == nil {
			s.verifiedRevision = r.Revision
			s.verifiedAt = time.Now().UTC()
		} else {
			s.verificationCode = codeOf(err)
		}
		s.mu.Unlock()
		return err
	})
}
func (s *Service) Attempt(ctx context.Context, id string) (Snapshot, error) {
	s.mu.Lock()
	if s.current != nil && s.current.record.ID == id {
		v := publicSnapshot(s.current.record)
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	r, err := s.store.Attempt(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	// A restarted/other API never claims to own a live QR. The persisted state is
	// enough to show the attempt; current QR/password material is only in memory.
	if !terminal(r.State) && time.Now().After(r.ExpiresAt) {
		r.State = "expired"
		r.Code = "expired"
		r.Seq++
		r.Revision++
	}
	return publicSnapshot(r), nil
}
func (s *Service) StartLogin(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Snapshot{}, Failure("unavailable")
	}
	if s.current != nil {
		select {
		case <-s.current.done:
		default:
			s.mu.Unlock()
			return Snapshot{}, ErrBusy
		}
	}
	ready := make(chan error, 1)
	active, cancel := context.WithTimeout(s.ctx, s.options.AttemptTTL)
	a := &attempt{record: AttemptRecord{Snapshot: Snapshot{ID: randomID(), Seq: 1, Revision: 1, State: "waiting_qr", ExpiresAt: time.Now().Add(s.options.AttemptTTL).UTC()}, Owner: randomID(), Namespace: "candidate_" + randomID()}, cancel: cancel, done: make(chan struct{})}
	s.current = a
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer close(a.done)
		defer cancel()
		sent := false
		err := s.coordinator.Do(active, false, func(run context.Context, lease Lease) error {
			promoted := false
			defer func() {
				if !promoted {
					_ = s.bridge.Discard(context.Background(), lease, a.record.Namespace)
				}
			}()
			r, err := s.store.Account(run)
			if err != nil {
				return err
			}
			s.mu.Lock()
			a.record.ExpectedRevision = r.Revision
			record := a.record
			s.mu.Unlock()
			if err = s.store.CreateAttempt(run, record); err != nil {
				return err
			}
			process, err := s.bridge.Login(run, lease, a.record.Namespace)
			if err != nil {
				return err
			}
			s.mu.Lock()
			a.process = process
			s.mu.Unlock()
			ready <- nil
			sent = true
			var identity string
			var failed error
			for event := range process.Events() {
				switch event.Type {
				case "qr":
					err = s.transition(run, a, "waiting_qr", "", event.QR, event.ExpiresAt)
				case "password_required":
					err = s.transition(run, a, "password_required", "", "", nil)
				case "password_invalid":
					err = s.transition(run, a, "password_required", "password_invalid", "", nil)
				case "authorized":
					identity = event.Identity
					err = s.transition(run, a, "verifying", "", "", nil)
				case "error":
					failed = Failure(event.Code)
				default:
					failed = Failure("invalid_bridge_output")
				}
				if err != nil {
					failed = err
					cancel()
				}
			}
			waitErr := process.Wait() // storage must be closed before the fresh process.
			if run.Err() != nil {
				return run.Err()
			}
			if failed != nil {
				return failed
			}
			if waitErr != nil {
				return waitErr
			}
			if !identityPattern.MatchString(identity) {
				return Failure("authorization_unconfirmed")
			}
			verify, cancelVerify := context.WithTimeout(run, s.options.VerifyTimeout)
			defer cancelVerify()
			confirmed, err := s.bridge.Verify(verify, lease, a.record.Namespace)
			if err != nil {
				return err
			}
			if confirmed != identity {
				return Failure("account_changed")
			}
			if run.Err() != nil {
				return run.Err()
			}
			s.mu.Lock()
			record = a.record
			s.mu.Unlock()
			account, err := s.store.Promote(run, record, confirmed, time.Now().UTC())
			if err != nil {
				return err
			}
			promoted = true
			s.mu.Lock()
			a.record.State = "connected"
			a.record.Code = ""
			a.record.Seq++
			a.record.Revision++
			a.record.QR = ""
			a.record.QRExpiresAt = nil
			s.verifiedRevision = account.Revision
			s.checkedRevision = account.Revision
			s.verificationCode = ""
			s.verifiedAt = account.VerifiedAt
			s.mu.Unlock()
			return nil
		})
		if err != nil {
			state := "failed"
			code := codeOf(err)
			if code == "cancelled" || code == "expired" {
				state = code
			}
			cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = s.transition(cleanup, a, state, code, "", nil)
		}
		if !sent {
			ready <- err
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			return Snapshot{}, err
		}
		return s.Attempt(ctx, a.record.ID)
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}
func (s *Service) transition(ctx context.Context, a *attempt, state, code, qr string, expires *time.Time) error {
	s.mu.Lock()
	a.record.State = state
	a.record.Code = code
	a.record.Seq++
	a.record.Revision++
	a.record.QR = qr
	a.record.QRExpiresAt = expires
	record := a.record
	s.mu.Unlock()
	// Store implementations receive no ephemeral login material.
	record.QR = ""
	record.QRExpiresAt = nil
	return s.store.UpdateAttempt(ctx, record)
}
func (s *Service) Password(ctx context.Context, id, password string) error {
	if len(password) < 1 || len(password) > 1024 {
		return Failure("invalid_input")
	}
	s.mu.Lock()
	a := s.current
	if a == nil || a.record.ID != id || a.record.State != "password_required" || time.Now().After(a.record.ExpiresAt) {
		s.mu.Unlock()
		return ErrConflict
	}
	process := a.process
	s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return process.Password(password)
}
func (s *Service) Cancel(ctx context.Context, id string) (Snapshot, error) {
	s.mu.Lock()
	a := s.current
	if a != nil && a.record.ID == id && !terminal(a.record.State) {
		a.cancel()
	}
	s.mu.Unlock()
	if a == nil || a.record.ID != id {
		return s.Attempt(ctx, id)
	}
	select {
	case <-a.done:
		return s.Attempt(ctx, id)
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}
func (s *Service) SnapshotForTask(ctx context.Context, messageURL string) (Input, error) {
	canonical, err := ParseMessageURL(messageURL)
	if err != nil {
		return Input{}, err
	}
	account, err := s.Account(ctx)
	if err != nil {
		return Input{}, err
	}
	if account.State != "connected" {
		return Input{}, Failure("auth_required")
	}
	r, err := s.store.Account(ctx)
	if err != nil {
		return Input{}, err
	}
	if r.Revision != account.Revision {
		return Input{}, Failure("account_changed")
	}
	return Input{MessageURL: canonical, AccountRevision: r.Revision, AccountIdentity: r.Identity}, nil
}

// Download waits for the account lock while the caller keeps Task Core heartbeat
// ownership. It returns only a completed verified source; Task Core owns publish.
func (s *Service) Download(ctx context.Context, input Input, outputDir string) (source Source, err error) {
	if err = ValidateInput(input); err != nil {
		return source, err
	}
	if !filepath.IsAbs(outputDir) {
		return source, Failure("invalid_output")
	}
	if err = os.Mkdir(outputDir, 0700); err != nil {
		return source, Failure("invalid_output")
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(outputDir)
		}
	}()
	err = s.coordinator.Do(ctx, true, func(run context.Context, lease Lease) error {
		r, e := s.store.Account(run)
		if e != nil {
			return e
		}
		if r.Revision != input.AccountRevision || r.Identity != input.AccountIdentity {
			return Failure("account_changed")
		}
		inspect, cancel := context.WithTimeout(run, s.options.VerifyTimeout)
		attachment, e := s.bridge.Inspect(inspect, lease, r.Namespace, input.MessageURL, s.options.MaxSourceBytes)
		cancel()
		if e != nil {
			return e
		}
		if attachment.Identity != input.AccountIdentity {
			return Failure("account_changed")
		}
		if attachment.Size < 1 || attachment.Size > s.options.MaxSourceBytes {
			return Failure("source_too_large")
		}
		if attachment.Extension != ".zip" && attachment.Extension != ".rar" && attachment.Extension != ".7z" {
			return Failure("unsupported_media")
		}
		if e = s.bridge.Download(run, lease, r.Namespace, input.MessageURL, outputDir, s.options.MaxSourceBytes); e != nil {
			return e
		}
		entries, e := os.ReadDir(outputDir)
		if e != nil || len(entries) != 1 || entries[0].Name() != "source" || entries[0].Type()&os.ModeSymlink != 0 {
			return Failure("download_incomplete")
		}
		path := filepath.Join(outputDir, "source")
		file, e := os.Open(path)
		if e != nil {
			return Failure("download_incomplete")
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() != attachment.Size || info.Size() > s.options.MaxSourceBytes {
			return Failure("download_incomplete")
		}
		hash := sha256.New()
		if _, e = io.Copy(hash, io.LimitReader(file, s.options.MaxSourceBytes+1)); e != nil {
			return e
		}
		if run.Err() != nil {
			return run.Err()
		}
		final := filepath.Join(outputDir, "source"+attachment.Extension)
		if e = os.Rename(path, final); e != nil {
			return e
		}
		source = Source{Path: final, Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)), Extension: attachment.Extension}
		return nil
	})
	if err == nil {
		success = true
	} else if codeOf(err) == "auth_required" {
		s.mu.Lock()
		if s.verifiedRevision == input.AccountRevision {
			s.verifiedRevision = 0
			s.checkedRevision = input.AccountRevision
			s.verificationCode = "auth_required"
		}
		s.mu.Unlock()
	}
	return source, err
}
