package httpapi

import (
	"math"
	"strings"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

func normalizeLogQuery(rawPage, rawPerPage, rawStatus, rawKeyword string) domain.LogQuery {
	keyword := strings.TrimSpace(rawKeyword)
	if len(keyword) > 120 {
		keyword = keyword[:120]
	}

	return domain.LogQuery{
		Page:    clampInt(parseInt(rawPage, 1), 1, math.MaxInt),
		PerPage: clampInt(parseInt(rawPerPage, domain.DefaultLogsPerPage), 1, domain.MaxLogsPerPage),
		Status:  normalizeStatusFilter(rawStatus),
		Keyword: keyword,
	}
}
