package httpv2

import (
	"fmt"
	"strings"
)

func buildTaskListFilter(status, q string) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)

	normalizedStatus := strings.ToUpper(strings.TrimSpace(status))
	if normalizedStatus != "" {
		args = append(args, normalizedStatus)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}

	normalizedQuery := strings.TrimSpace(q)
	if normalizedQuery != "" {
		args = append(args, normalizedQuery)
		placeholder := len(args)
		clauses = append(
			clauses,
			fmt.Sprintf(
				"(canonical_url ILIKE '%%' || $%d || '%%' OR url ILIKE '%%' || $%d || '%%')",
				placeholder,
				placeholder,
			),
		)
	}

	return strings.Join(clauses, " AND "), args
}
