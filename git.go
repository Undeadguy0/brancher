package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runGit запускает git с аргументами args в директории dir и возвращает stdout.
// Если git завершился с ошибкой, в тексте ошибки будет его stderr.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Не даём git зависнуть на запросе логина/пароля (например, при ls-remote).
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && msg != "" {
			return stdout.String(), fmt.Errorf("Ошибка git %s: %s", strings.Join(args, " "), msg)
		}
		return stdout.String(), fmt.Errorf("Ошибка запуска git %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

// splitLines делит вывод на строки, обрезая пробелы (и \r на Windows) и убирая пустые.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// listRemotes возвращает список remote из `git remote`.
func listRemotes(dir string) ([]string, error) {
	out, err := runGit(dir, "remote")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// detectHeadBranch определяет HEAD-ветку remote:
// сначала локально через refs/remotes/<remote>/HEAD,
// затем через `git ls-remote --symref <remote> HEAD`.
func detectHeadBranch(dir, remote string) (string, error) {
	out, err := runGit(dir, "symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		if branch, ok := strings.CutPrefix(ref, "refs/remotes/"+remote+"/"); ok && branch != "" {
			return branch, nil
		}
	}

	out, err = runGit(dir, "ls-remote", "--symref", "--", remote, "HEAD")
	if err != nil {
		return "", err
	}
	if branch, ok := parseLsRemoteSymref(out); ok {
		return branch, nil
	}

	return "", fmt.Errorf("не удалось определить HEAD-ветку remote %q", remote)
}

// parseLsRemoteSymref ищет строку вида "ref: refs/heads/<branch>\tHEAD".
func parseLsRemoteSymref(out string) (string, bool) {
	for _, line := range splitLines(out) {
		rest, ok := strings.CutPrefix(line, "ref:")
		if !ok {
			continue
		}
		ref, name, ok := strings.Cut(strings.TrimSpace(rest), "\t")
		if !ok || strings.TrimSpace(name) != "HEAD" {
			continue
		}
		if branch, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/"); ok && branch != "" {
			return branch, true
		}
	}
	return "", false
}
