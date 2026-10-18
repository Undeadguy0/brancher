package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const branchesFileName = ".branches"

type Target struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

type Targets struct {
	Targets []Target `json:"targets"`
}

func (t *Targets) Add(targ Target) {
	t.Targets = append(t.Targets, targ)
}

// Index возвращает индекс цели с данным remote или -1.
func (t *Targets) Index(remote string) int {
	for i, targ := range t.Targets {
		if targ.Remote == remote {
			return i
		}
	}
	return -1
}

// Remove удаляет цель с данным remote, сохраняя порядок остальных.
func (t *Targets) Remove(remote string) bool {
	i := t.Index(remote)
	if i < 0 {
		return false
	}
	t.Targets = append(t.Targets[:i], t.Targets[i+1:]...)
	return true
}

func branchesPath(root string) string {
	return filepath.Join(root, branchesFileName)
}

// loadTargets читает .branches. exists=false, если файла нет.
func loadTargets(root string) (targets Targets, exists bool, err error) {
	path := branchesPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Targets{Targets: []Target{}}, false, nil
	}
	if err != nil {
		return Targets{}, false, fmt.Errorf("Ошибка чтения %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &targets); err != nil {
		return Targets{}, true, fmt.Errorf("Ошибка разбора %q: %w", path, err)
	}
	if targets.Targets == nil {
		targets.Targets = []Target{}
	}
	return targets, true, nil
}

func marshalTargets(targets Targets) ([]byte, error) {
	if targets.Targets == nil {
		targets.Targets = []Target{}
	}
	data, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("Ошибка на этапе сериализации: %w", err)
	}
	return append(data, '\n'), nil
}

// saveTargets пишет .branches через временный файл и переименование,
// чтобы при ошибке не оставить полузаписанный файл.
func saveTargets(root string, targets Targets) error {
	data, err := marshalTargets(targets)
	if err != nil {
		return err
	}

	path := branchesPath(root)
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}

	tmp, err := os.CreateTemp(root, branchesFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("Ошибка создания временного файла: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("Ошибка записи %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("Ошибка записи %q: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("Ошибка установки прав %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("Ошибка записи %q: %w", path, err)
	}
	return nil
}
