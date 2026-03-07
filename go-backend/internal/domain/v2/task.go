package v2

type Status string

const (
	StatusQueued          Status = "QUEUED"
	StatusRunning         Status = "RUNNING"
	StatusCancelRequested Status = "CANCEL_REQUESTED"
	StatusSuccess         Status = "SUCCESS"
	StatusFailed          Status = "FAILED"
	StatusCanceled        Status = "CANCELED"
)

type Task struct {
	ID     string
	Status Status
}

func (t *Task) TransitionTo(next Status) error {
	if err := ValidateTransition(t.Status, next); err != nil {
		return err
	}

	t.Status = next
	return nil
}
