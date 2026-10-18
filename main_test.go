package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isolateGit отключает глобальный и системный конфиг git, чтобы тесты не зависели от окружения.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git не найден в PATH")
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[protocol \"file\"]\n\tallow = always\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// newBareRemote создаёт локальный bare-репозиторий с одним коммитом, HEAD которого указывает на head.
func newBareRemote(t *testing.T, head string) string {
	t.Helper()
	base := t.TempDir()
	bare := filepath.Join(base, "remote.git")
	git(t, base, "init", "--bare", "-q", "-b", head, bare)

	seed := filepath.Join(base, "seed")
	git(t, base, "init", "-q", "-b", head, seed)
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", "README")
	git(t, seed, "commit", "-q", "-m", "seed")
	git(t, seed, "push", "-q", bare, head)
	return bare
}

// newRepo создаёт пустой рабочий репозиторий без remote.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	git(t, filepath.Dir(dir), "init", "-q", "-b", "master", dir)
	return dir
}

// cloneRepo клонирует bare-репозиторий (так появляется refs/remotes/origin/HEAD).
func cloneRepo(t *testing.T, bare string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	git(t, filepath.Dir(dir), "clone", "-q", bare, dir)
	return dir
}

type result struct {
	stdout, stderr string
	err            error
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return result{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func mustRun(t *testing.T, args ...string) result {
	t.Helper()
	r := run(t, args...)
	if r.err != nil {
		t.Fatalf("brancher %v: %v\nstdout: %s\nstderr: %s", args, r.err, r.stdout, r.stderr)
	}
	return r
}

func readTargets(t *testing.T, root string) []Target {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".branches"))
	if err != nil {
		t.Fatal(err)
	}
	var targets Targets
	if err := json.Unmarshal(data, &targets); err != nil {
		t.Fatalf("невалидный JSON: %v\n%s", err, data)
	}
	return targets.Targets
}

func writeTargets(t *testing.T, root string, targets ...Target) {
	t.Helper()
	if err := saveTargets(root, Targets{Targets: targets}); err != nil {
		t.Fatal(err)
	}
}

func assertTargets(t *testing.T, root string, want ...Target) {
	t.Helper()
	got := readTargets(t, root)
	if want == nil {
		want = []Target{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
}

func assertNoFile(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".branches")); !os.IsNotExist(err) {
		t.Fatalf("файл .branches не должен существовать (err=%v)", err)
	}
}

func TestInitDetectsHeadViaSymbolicRef(t *testing.T) {
	isolateGit(t)
	bare := newBareRemote(t, "trunk")
	repo := cloneRepo(t, bare)

	// Меняем HEAD у bare: локальный refs/remotes/origin/HEAD всё ещё trunk,
	// значит ветка берётся именно из symbolic-ref, без обращения к remote.
	git(t, bare, "branch", "other", "trunk")
	git(t, bare, "symbolic-ref", "HEAD", "refs/heads/other")

	mustRun(t, "-p", repo)
	assertTargets(t, repo, Target{Remote: "origin", Branch: "trunk"})
}

func TestInitFallsBackToLsRemote(t *testing.T) {
	isolateGit(t)
	bare := newBareRemote(t, "develop")
	repo := newRepo(t)
	git(t, repo, "remote", "add", "upstream", bare) // без fetch: refs/remotes/upstream/HEAD нет

	r := mustRun(t, "-p", repo)
	assertTargets(t, repo, Target{Remote: "upstream", Branch: "develop"})
	if r.stderr != "" {
		t.Fatalf("неожиданное предупреждение: %q", r.stderr)
	}
}

func TestInitFallsBackToDefaultBranch(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.git")
	git(t, repo, "remote", "add", "broken", missing)

	r := mustRun(t, "-p", repo, "-b", "main")
	assertTargets(t, repo, Target{Remote: "broken", Branch: "main"})
	if !strings.Contains(r.stderr, "неизвестна HEAD-ветка") {
		t.Fatalf("ожидалось предупреждение, stderr: %q", r.stderr)
	}

	r = mustRun(t, "-p", repo, "-b", "main", "-r", "-s")
	if r.stderr != "" || r.stdout != "" {
		t.Fatalf("в --silent не должно быть вывода: stdout=%q stderr=%q", r.stdout, r.stderr)
	}
}

func TestInitForceBranch(t *testing.T) {
	isolateGit(t)
	bare := newBareRemote(t, "main")
	repo := cloneRepo(t, bare)
	git(t, repo, "remote", "add", "second", bare)

	mustRun(t, "-p", repo, "-f", "feature/x")
	assertTargets(t, repo,
		Target{Remote: "origin", Branch: "feature/x"},
		Target{Remote: "second", Branch: "feature/x"},
	)
}

func TestInitHonorsProjectRoot(t *testing.T) {
	isolateGit(t)
	bare := newBareRemote(t, "main")
	other := newRepo(t) // cwd: другой git-репозиторий без remote
	t.Chdir(other)

	t.Run("flag", func(t *testing.T) {
		repo := cloneRepo(t, bare)
		mustRun(t, "--project", repo)
		assertTargets(t, repo, Target{Remote: "origin", Branch: "main"})
		assertNoFile(t, other)
	})

	t.Run("positional", func(t *testing.T) {
		repo := cloneRepo(t, bare)
		mustRun(t, repo)
		assertTargets(t, repo, Target{Remote: "origin", Branch: "main"})
		assertNoFile(t, other)
	})

	t.Run("cwd", func(t *testing.T) {
		repo := cloneRepo(t, bare)
		t.Chdir(repo)
		mustRun(t)
		assertTargets(t, repo, Target{Remote: "origin", Branch: "main"})
	})

	t.Run("conflict", func(t *testing.T) {
		repo := cloneRepo(t, bare)
		if r := run(t, "-p", repo, other); r.err == nil {
			t.Fatal("ожидалась ошибка при разных -p и [path]")
		}
	})
}

func TestInitNoRemotes(t *testing.T) {
	isolateGit(t)
	repo := newRepo(t)

	r := mustRun(t, "-p", repo)
	if !strings.Contains(r.stdout, "нет установленных удаленных репозиториев") {
		t.Fatalf("stdout: %q", r.stdout)
	}
	assertNoFile(t, repo)
}

func TestInitExistingFile(t *testing.T) {
	isolateGit(t)
	bare := newBareRemote(t, "main")
	repo := cloneRepo(t, bare)
	writeTargets(t, repo, Target{Remote: "origin", Branch: "old"})

	if r := run(t, "-p", repo); r.err == nil || !strings.Contains(r.err.Error(), "уже существует") {
		t.Fatalf("ожидалась ошибка о существующем файле, got %v", r.err)
	}
	assertTargets(t, repo, Target{Remote: "origin", Branch: "old"})

	mustRun(t, "-p", repo, "-r")
	assertTargets(t, repo, Target{Remote: "origin", Branch: "main"})
}

func TestNotAGitRepoSurfacesStderr(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	r := run(t, "-p", dir)
	if r.err == nil || !strings.Contains(strings.ToLower(r.err.Error()), "not a git repository") {
		t.Fatalf("ожидалась ошибка git со stderr, got %v", r.err)
	}
}

func TestJSONFormat(t *testing.T) {
	data, err := marshalTargets(Targets{Targets: []Target{
		{Remote: "github", Branch: "master"},
		{Remote: "gitlab", Branch: "main"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "targets": [
    {
      "remote": "github",
      "branch": "master"
    },
    {
      "remote": "gitlab",
      "branch": "main"
    }
  ]
}
`
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}

	empty, err := marshalTargets(Targets{})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != "{\n  \"targets\": []\n}\n" {
		t.Fatalf("пустой список: %q", empty)
	}
}

func TestSaveTargetsLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	writeTargets(t, dir, Target{Remote: "a", Branch: "b"})
	writeTargets(t, dir, Target{Remote: "a", Branch: "c"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".branches" {
		t.Fatalf("лишние файлы: %v", entries)
	}
	assertTargets(t, dir, Target{Remote: "a", Branch: "c"})
}

func TestParseLsRemoteSymref(t *testing.T) {
	cases := map[string]string{
		"ref: refs/heads/main\tHEAD\n0123abcd\tHEAD\n":     "main",
		"ref: refs/heads/feature/x\tHEAD\r\nabc\tHEAD\r\n": "feature/x",
		"0123abcd\tHEAD\n":                    "",
		"ref: refs/heads/dev\trefs/heads/x\n": "",
		"":                                    "",
	}
	for in, want := range cases {
		got, ok := parseLsRemoteSymref(in)
		if got != want || ok != (want != "") {
			t.Errorf("parseLsRemoteSymref(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
