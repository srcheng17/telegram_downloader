package v2

import "fmt"

var allowed = map[Status]map[Status]struct{}{
	StatusQueued:  {StatusRunning: {}},
	StatusRunning: {StatusSuccess: {}, StatusFailed: {}, StatusCanceled: {}},
}

func CanTransition(from, to Status) bool {
	nextStates, ok := allowed[from]
	if !ok {
		return false
	}

	_, ok = nextStates[to]
	return ok
}

func ValidateTransition(from, to Status) error {
	if CanTransition(from, to) {
		return nil
	}

	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}
