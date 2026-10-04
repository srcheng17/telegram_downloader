package telegram

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
var identityPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ParseMessageURL accepts one exact post, including forum topic links. Query
// selectors and comment/discussion expansion are intentionally not in the MVP.
func ParseMessageURL(raw string) (string, error) {
	if len(raw) > 512 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\\r\n\t%") {
		return "", Failure("invalid_message_url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "t.me" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", Failure("invalid_message_url")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	start := 1
	if len(parts) < 2 {
		return "", Failure("invalid_message_url")
	}
	if parts[0] == "c" {
		if len(parts) < 3 || len(parts) > 4 {
			return "", Failure("invalid_message_url")
		}
		start = 1
	} else {
		if len(parts) > 3 || !usernamePattern.MatchString(parts[0]) {
			return "", Failure("invalid_message_url")
		}
		parts[0] = strings.ToLower(parts[0])
	}
	for offset, part := range parts[start:] {
		id, e := strconv.ParseInt(part, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != part {
			return "", Failure("invalid_message_url")
		}
		// Telegram message/topic IDs are TL int32; private channel IDs are int64.
		if !(parts[0] == "c" && offset == 0) && id > math.MaxInt32 {
			return "", Failure("invalid_message_url")
		}
	}
	// Official tutil resolves a forum link by its final message ID. Canonicalize
	// away the topic segment so equivalent links cannot create duplicate tasks.
	if parts[0] == "c" && len(parts) == 4 {
		parts = []string{parts[0], parts[1], parts[3]}
	}
	if parts[0] != "c" && len(parts) == 3 {
		parts = []string{parts[0], parts[2]}
	}
	return "https://t.me/" + strings.Join(parts, "/"), nil
}
func ValidateInput(input Input) error {
	canonical, err := ParseMessageURL(input.MessageURL)
	if err != nil {
		return err
	}
	if canonical != input.MessageURL || input.AccountRevision < 1 || !identityPattern.MatchString(input.AccountIdentity) {
		return Failure("invalid_source")
	}
	return nil
}
