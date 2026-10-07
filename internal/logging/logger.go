package logging

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|auth|credential|apikey|api_key|access_key|private_key)`)
	bearerPattern       = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	kvSecretPattern     = regexp.MustCompile(`(?i)(password|passwd|secret|token|apikey|api_key|access_key|private_key)\s*[:=]\s*["']?([^\s"',;]+)["']?`)
)

// RedactionHandler wraps an slog.Handler and redacts values for keys matching sensitive patterns.
type RedactionHandler struct {
	slog.Handler
}

// NewRedactionHandler creates a new RedactionHandler wrapping an existing slog.Handler.
func NewRedactionHandler(h slog.Handler) *RedactionHandler {
	return &RedactionHandler{Handler: h}
}

// Handle inspects record attributes and redacts sensitive information.
func (h *RedactionHandler) Handle(ctx context.Context, r slog.Record) error {
	redactedRecord := slog.NewRecord(r.Time, r.Level, RedactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		redactedRecord.AddAttrs(redactAttr(a))
		return true
	})
	return h.Handler.Handle(ctx, redactedRecord)
}

// WithAttrs returns a new RedactionHandler with additional attributes.
func (h *RedactionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactionHandler{Handler: h.Handler.WithAttrs(redacted)}
}

// WithGroup returns a new RedactionHandler with group scoping.
func (h *RedactionHandler) WithGroup(name string) slog.Handler {
	return &RedactionHandler{Handler: h.Handler.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if sensitiveKeyPattern.MatchString(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}

	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactString(a.Value.String()))
	case slog.KindGroup:
		attrs := a.Value.Group()
		redactedGroup := make([]slog.Attr, len(attrs))
		for i, sub := range attrs {
			redactedGroup[i] = redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redactedGroup...)}
	default:
		return a
	}
}

// RedactString sanitizes known secret patterns inside strings, such as basic auth URLs or token headers.
func RedactString(s string) string {
	// Redact basic auth in URLs: scheme://user:pass@host -> scheme://user:[REDACTED]@host
	if strings.Contains(s, "://") && strings.Contains(s, "@") {
		s = RedactURL(s)
	}
	s = bearerPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = kvSecretPattern.ReplaceAllString(s, "${1}=[REDACTED]")
	return s
}

// RedactURL strips passwords and userInfo credentials from URL strings.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
		} else if u.User.Username() != "" {
			u.User = url.User("[REDACTED]")
		}
		return strings.ReplaceAll(u.String(), "%5BREDACTED%5D", "[REDACTED]")
	}
	return rawURL
}

// NewLogger creates a structured slog.Logger writing JSON to the specified writer with redaction.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	baseHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(NewRedactionHandler(baseHandler))
}

// DefaultLogger creates a standard structured JSON logger writing to os.Stdout.
func DefaultLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	return NewLogger(os.Stdout, level)
}
