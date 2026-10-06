package tasks

// KomgaDeliveryResult distinguishes durable file copy from its asynchronous
// catalog projection. A pending result can resume through the same action.
type KomgaDeliveryResult struct {
	TargetPath string `json:"target_path"`
	Copied     bool   `json:"copy_completed"`
	Status     string `json:"delivery_status"`
	Indexed    string `json:"komga_indexed"`
	LibraryID  string `json:"library_id,omitempty"`
	BookID     string `json:"book_id,omitempty"`
	Reason     string `json:"pending_reason,omitempty"`
}
