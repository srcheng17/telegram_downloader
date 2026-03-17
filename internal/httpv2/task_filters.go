package httpv2

import "strings"

func buildTaskListFilter(status, q string) (string, []any) {
	normalizedStatus := strings.ToUpper(strings.TrimSpace(status))
	normalizedQuery := strings.TrimSpace(q)

	switch {
	case normalizedStatus == "" && normalizedQuery == "":
		return "", nil
	case normalizedStatus != "" && normalizedQuery == "":
		return "status = $1", []any{normalizedStatus}
	case normalizedStatus == "" && normalizedQuery != "":
		return "(canonical_url ILIKE '%' || $1 || '%' OR url ILIKE '%' || $1 || '%')", []any{normalizedQuery}
	default:
		return "status = $1 AND (canonical_url ILIKE '%' || $2 || '%' OR url ILIKE '%' || $2 || '%')", []any{normalizedStatus, normalizedQuery}
	}
}
