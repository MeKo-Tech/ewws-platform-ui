package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// Logging emits one slog access-log line per request.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lw := &loggedWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(lw, r)

			logger.Info(
				"http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", lw.status),
				slog.Int("bytes", lw.bytes),
				slog.Duration("dur", time.Since(start)),
				slog.String("ua", r.UserAgent()),
			)
		})
	}
}

type loggedWriter struct {
	http.ResponseWriter

	status int
	bytes  int
}

func (l *loggedWriter) WriteHeader(code int) {
	l.status = code
	l.ResponseWriter.WriteHeader(code)
}

func (l *loggedWriter) Write(b []byte) (int, error) {
	n, err := l.ResponseWriter.Write(b)
	l.bytes += n
	return n, err
}
