package grpcproxy

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/plhhao/faultline/internal/engine"
)

func Metadata(r *http.Request) (engine.Metadata, bool) {
	content := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	service, method, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	valid := r.ProtoMajor == 2 && r.Method == "POST" && (content == "application/grpc" || strings.HasPrefix(content, "application/grpc+")) && ok && service != "" && method != "" && !strings.Contains(method, "/")
	return engine.Metadata{Service: service, Method: method, Path: r.URL.Path, Headers: r.Header}, valid
}

// Timeout decodes the relative deadline carried by gRPC without inspecting messages.
func Timeout(value string) (time.Duration, bool) {
	if len(value) < 2 || len(value) > 9 {
		return 0, false
	}
	units := map[byte]time.Duration{'H': time.Hour, 'M': time.Minute, 'S': time.Second, 'm': time.Millisecond, 'u': time.Microsecond, 'n': time.Nanosecond}
	for _, digit := range value[:len(value)-1] {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	unit, ok := units[value[len(value)-1]]
	n, err := strconv.ParseUint(value[:len(value)-1], 10, 32)
	if !ok || err != nil {
		return 0, false
	}
	if n > uint64((1<<63-1)/unit) {
		return time.Duration(1<<63 - 1), true
	}
	return time.Duration(n) * unit, true
}

func Error(w http.ResponseWriter, status, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", status)
	w.Header().Set("Grpc-Message", message)
	w.WriteHeader(http.StatusOK)
}

func TimeoutHeader(duration time.Duration) string {
	duration = max(time.Nanosecond, duration)
	for _, unit := range []struct {
		size   time.Duration
		suffix string
	}{{time.Nanosecond, "n"}, {time.Microsecond, "u"}, {time.Millisecond, "m"}, {time.Second, "S"}, {time.Minute, "M"}, {time.Hour, "H"}} {
		n := duration / unit.size
		if duration%unit.size != 0 {
			n++
		}
		if n <= 99999999 {
			return strconv.FormatInt(int64(n), 10) + unit.suffix
		}
	}
	panic("time.Duration cannot exceed gRPC hour range")
}
