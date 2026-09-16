package remote

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

type user struct {
	Role string `json:"role"`
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

var userName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

func readUsers(dir string) (map[string]user, error) {
	data, err := os.ReadFile(filepath.Join(dir, "users.json"))
	if err != nil {
		return nil, err
	}
	var users map[string]user
	if err := json.Unmarshal(data, &users); err != nil || users == nil {
		return nil, errors.New("invalid users file")
	}
	return users, nil
}
func passwordHash(password string, salt []byte) string {
	key, _ := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return hex.EncodeToString(key)
}
func verifyPassword(u user, password string) bool {
	salt, err := hex.DecodeString(u.Salt)
	if err != nil || len(salt) != 16 {
		salt = make([]byte, 16)
	}
	hash := passwordHash(password, salt)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(u.Hash)) == 1 && (u.Role == "viewer" || u.Role == "editor")
}

// SetUser atomically updates operator-owned accounts; replacing a hash revokes sessions.
func SetUser(dir, name, role, password string, remove bool) error {
	if !userName.MatchString(name) {
		return errors.New("username: use 1–64 letters, digits, dot, underscore or hyphen")
	}
	if !remove && (role != "viewer" && role != "editor" || len(password) < 12 || len(password) > 1024) {
		return errors.New("role must be viewer/editor; password must be 12–1024 bytes")
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "users.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	users, err := readUsers(dir)
	if errors.Is(err, os.ErrNotExist) {
		users = map[string]user{}
	} else if err != nil {
		return err
	}
	if remove {
		delete(users, name)
	} else {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return err
		}
		users[name] = user{role, hex.EncodeToString(salt), passwordHash(password, salt)}
	}
	data, err := json.Marshal(users)
	if err != nil {
		return err
	}
	_, err = atomicFile(filepath.Join(dir, "users.json"), data)
	return err
}
