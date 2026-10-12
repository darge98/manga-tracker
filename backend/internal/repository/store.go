package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Manga struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	CoverURL    string   `json:"coverUrl"`
	Chapters    *int     `json:"chapters"`
	Status      string   `json:"status"`
	Genres      []string `json:"genres"`
	Score       *float64 `json:"score"`
	Author      string   `json:"author"`
	URL         string   `json:"url"`
}
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	PasswordHash string `json:"passwordHash"`
}
type Session struct {
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Chapter struct {
	Chapter string     `json:"chapter"`
	Read    bool       `json:"read"`
	ReadAt  *time.Time `json:"readAt"`
	Rating  *int       `json:"rating"`
	Notes   string     `json:"notes"`
}
type Library struct {
	MangaID  int       `json:"mangaId"`
	Chapters []Chapter `json:"chapters"`
	ToRead   bool      `json:"toRead"`
	Favorite bool      `json:"favorite"`
}
type List struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"ownerId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsPublic    bool      `json:"isPublic"`
	ItemIDs     []int     `json:"itemIds"`
	CreatedAt   time.Time `json:"createdAt"`
}
type State struct {
	Users     map[string]User            `json:"users"`
	Sessions  map[string]Session         `json:"sessions"`
	Manga     map[int]Manga              `json:"manga"`
	Libraries map[string]map[int]Library `json:"libraries"`
	Lists     map[string]List            `json:"lists"`
}

func empty() State {
	return State{map[string]User{}, map[string]Session{}, map[int]Manga{}, map[string]map[int]Library{}, map[string]List{}}
}

type Store struct {
	mu    sync.RWMutex
	path  string
	state State
	lock  *os.File
	db    *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("database already in use by another server")
	}
	s := &Store{path: path, state: empty(), lock: lock}
	success := false
	defer func() {
		if !success {
			s.Close()
		}
	}()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		success = true
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.state); err != nil {
		return nil, err
	}
	if s.state.Users == nil || s.state.Sessions == nil || s.state.Manga == nil || s.state.Libraries == nil || s.state.Lists == nil {
		return nil, errors.New("invalid database structure")
	}
	success = true
	return s, nil
}
func (s *Store) View(fn func(State)) error {
	if s.db != nil {
		return s.viewPostgres(fn)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.state)
	return nil
}

// Update serializes mutations, saves a complete snapshot via fsync + atomic rename,
// and publishes it only after persistence succeeds. A failed write cannot mutate live state.
func (s *Store) Update(fn func(*State) error) error {
	if s.db != nil {
		return s.updatePostgres(fn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var next State
	if err = json.Unmarshal(b, &next); err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	b, err = json.Marshal(next)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".database-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, s.path); err != nil {
		return err
	}
	s.state = next
	if dir, e := os.Open(filepath.Dir(s.path)); e == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	if s.lock == nil {
		return nil
	}
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	return s.lock.Close()
}
