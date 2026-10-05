package taskcore

import (
	"fmt"
	"strings"
)

type Kind string

type Status string

type Actor string

type Action string

const (
	KindURL      Kind = "url"
	KindUpload   Kind = "upload"
	KindTelegram Kind = "telegram"
)

const (
	StatusCreated   Status = "CREATED"
	StatusReady     Status = "READY"
	StatusRunning   Status = "RUNNING"
	StatusCanceling Status = "CANCELING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCanceled  Status = "CANCELED"
)

const (
	ActorAPI      Actor = "api"
	ActorWorker   Actor = "worker"
	ActorRecovery Actor = "recovery"
)

const (
	ActionCancel      Action = "cancel"
	ActionRetry       Action = "retry"
	ActionDownload    Action = "download"
	ActionCopyToKomga Action = "copy_to_komga"
)

func NormalizeStatus(value string) Status {
	return Status(strings.ToUpper(strings.TrimSpace(value)))
}

func NormalizeKind(value string) Kind {
	return Kind(strings.ToLower(strings.TrimSpace(value)))
}

func IsTerminal(status Status) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func CanCancel(status Status) bool {
	switch status {
	case StatusCreated, StatusReady, StatusRunning:
		return true
	default:
		return false
	}
}

func CanRetry(status Status, hasSource bool) bool {
	if !hasSource {
		return false
	}
	switch status {
	case StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

func CanAccessResult(status Status, hasResult bool) bool {
	return status == StatusSucceeded && hasResult
}

func AvailableActions(status Status, hasSource bool, hasResult bool, komgaConfigured bool) []Action {
	actions := make([]Action, 0, 4)
	if CanCancel(status) {
		actions = append(actions, ActionCancel)
	}
	if CanRetry(status, hasSource) {
		actions = append(actions, ActionRetry)
	}
	if CanAccessResult(status, hasResult) {
		actions = append(actions, ActionDownload)
		if komgaConfigured {
			actions = append(actions, ActionCopyToKomga)
		}
	}
	return actions
}

func ValidateTransition(from Status, to Status, actor Actor) error {
	if transitionAllowed(from, to, actor) {
		return nil
	}
	return fmt.Errorf("invalid taskcore transition: %s -> %s by %s", from, to, actor)
}

func transitionAllowed(from Status, to Status, actor Actor) bool {
	switch from {
	case StatusCreated:
		switch to {
		case StatusReady:
			return actor == ActorAPI
		case StatusCanceling:
			return actor == ActorAPI
		default:
			return false
		}
	case StatusReady:
		switch to {
		case StatusRunning:
			return actor == ActorWorker
		case StatusCanceling:
			return actor == ActorAPI
		case StatusFailed:
			return actor == ActorAPI
		default:
			return false
		}
	case StatusRunning:
		switch to {
		case StatusSucceeded:
			return actor == ActorWorker
		case StatusFailed:
			return actor == ActorWorker || actor == ActorRecovery
		case StatusCanceling:
			return actor == ActorAPI
		case StatusReady:
			return actor == ActorRecovery
		default:
			return false
		}
	case StatusCanceling:
		return to == StatusCanceled && (actor == ActorWorker || actor == ActorRecovery || actor == ActorAPI)
	case StatusFailed:
		return to == StatusReady && actor == ActorAPI
	case StatusCanceled:
		return to == StatusReady && actor == ActorAPI
	case StatusSucceeded:
		return false
	default:
		return false
	}
}
