package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestAtomicPersistenceConcurrentUpdatesAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := Open(path); e == nil {
		second.Close()
		t.Fatal("second writer accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if e := s.Update(func(st *State) error {
				key := strconv.Itoa(i)
				st.Users[key] = User{ID: key, Email: key + "@test.dev"}
				return nil
			}); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	e = s.Update(func(st *State) error { st.Users["rollback"] = User{}; return errors.New("cancel") })
	if e == nil {
		t.Fatal("mutation should fail")
	}
	s.View(func(st State) {
		if len(st.Users) != 30 {
			t.Fatal("lost mutation or rollback leaked")
		}
	})
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe database permissions")
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.View(func(st State) {
		if len(st.Users) != 30 {
			t.Fatal("data did not survive restart")
		}
	})
}
func TestCorruptDatabaseFailsClosedAndFailedSaveRollsBack(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{oops"), 0600)
	if s, e := Open(bad); e == nil {
		s.Close()
		t.Fatal("corrupt database accepted")
	}
	path := filepath.Join(dir, "database.json")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	os.Mkdir(path, 0700)
	if e = s.Update(func(st *State) error { st.Users["x"] = User{ID: "x"}; return nil }); e == nil {
		t.Fatal("save must fail")
	}
	s.View(func(st State) {
		if len(st.Users) != 0 {
			t.Fatal("failed save altered state")
		}
	})
}
