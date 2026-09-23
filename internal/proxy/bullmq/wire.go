package bullmq

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
)

const (
	maxFrame    = 1 << 20
	maxPending  = 128
	standardSHA = "808626431266f2e834f108cac6b35ce556927132"
)

var errProtocol = errors.New("bullmq: unsupported or malformed RESP")

type frame struct {
	kind  byte
	value []byte
	items []frame
	wire  []byte
}

func readFrame(r *bufio.Reader) (frame, error) {
	var wire []byte
	nodes := 0
	var read func(int) (frame, error)
	read = func(depth int) (frame, error) {
		nodes++
		if depth > 8 || nodes > 8192 {
			return frame{}, errProtocol
		}
		var line []byte
		for {
			part, err := r.ReadSlice('\n')
			if len(wire)+len(line)+len(part) > maxFrame {
				return frame{}, errProtocol
			}
			line = append(line, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				return frame{}, err
			}
			break
		}
		if len(line) < 3 || line[len(line)-2] != '\r' {
			return frame{}, errProtocol
		}
		wire = append(wire, line...)
		f := frame{kind: line[0], value: line[1 : len(line)-2]}
		switch f.kind {
		case '+', '-':
			if bytes.ContainsAny(f.value, "\r\n") {
				return frame{}, errProtocol
			}
		case ':':
			if _, err := strconv.ParseInt(string(f.value), 10, 64); err != nil {
				return frame{}, errProtocol
			}
		case '$', '*':
			n, err := strconv.ParseInt(string(f.value), 10, 64)
			if err != nil || n < -1 {
				return frame{}, errProtocol
			}
			f.value = nil
			if n == -1 {
				return f, nil
			}
			if f.kind == '$' {
				if n > int64(maxFrame-len(wire)-2) {
					return frame{}, errProtocol
				}
				data := make([]byte, int(n)+2)
				if _, err := io.ReadFull(r, data); err != nil {
					return frame{}, err
				}
				if data[len(data)-2] != '\r' || data[len(data)-1] != '\n' {
					return frame{}, errProtocol
				}
				wire = append(wire, data...)
				f.value = data[:len(data)-2]
			} else {
				if n > 4096 {
					return frame{}, errProtocol
				}
				f.items = make([]frame, 0, int(n))
				for i := int64(0); i < n; i++ {
					item, err := read(depth + 1)
					if err != nil {
						return frame{}, err
					}
					f.items = append(f.items, item)
				}
			}
		default:
			return frame{}, errProtocol
		}
		return f, nil
	}
	f, err := read(0)
	f.wire = wire
	return f, err
}

func command(f frame) ([]string, error) {
	if f.kind != '*' || len(f.items) == 0 {
		return nil, errProtocol
	}
	args := make([]string, len(f.items))
	for i, item := range f.items {
		if item.kind != '$' || item.value == nil {
			return nil, errProtocol
		}
		args[i] = string(item.value)
	}
	args[0] = strings.ToUpper(args[0])
	switch args[0] {
	case "MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH", "HELLO", "SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE", "UNSUBSCRIBE", "PUNSUBSCRIBE", "SUNSUBSCRIBE", "MONITOR", "SELECT", "SYNC", "PSYNC", "REPLCONF":
		return nil, errProtocol
	case "CLIENT":
		if len(args) < 2 {
			return nil, errProtocol
		}
		switch strings.ToUpper(args[1]) {
		case "REPLY", "TRACKING":
			return nil, errProtocol
		}
	}
	return args, nil
}

func queueName(args []string) string {
	if len(args) != 15 || args[2] != "9" {
		return ""
	}
	var hash string
	switch args[0] {
	case "EVAL":
		sum := sha1.Sum([]byte(args[1]))
		hash = hex.EncodeToString(sum[:])
	case "EVALSHA":
		hash = strings.ToLower(args[1])
	default:
		return ""
	}
	if hash != standardSHA {
		return ""
	}
	key := strings.Split(args[3], ":")
	if len(key) != 3 || key[0] != "bull" || !safeQueue(key[1]) {
		return ""
	}
	for i, suffix := range []string{"wait", "paused", "meta", "id", "completed", "delayed", "active", "events", "marker"} {
		if args[3+i] != "bull:"+key[1]+":"+suffix {
			return ""
		}
	}
	return key[1]
}

func safeQueue(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
