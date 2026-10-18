package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// options — значения флагов. Создаются на каждый newRootCmd, без глобального состояния.
type options struct {
	defaultBranch string
	projectRoot   string
	forceBranch   string
	silent        bool
	rewrite       bool
	removeGit     bool
}

// resolveRoot определяет корень проекта: флаг -p, затем позиционный [path], затем текущая директория.
func resolveRoot(project string, args []string) (string, error) {
	if len(args) > 1 {
		return "", errors.New("Ожидается максимум 1 позиционный аргумент")
	}

	path := project
	if len(args) == 1 {
		if path != "" && !samePath(path, args[0]) {
			return "", fmt.Errorf("Путь к проекту указан дважды (-p %q и %q). Укажите что-то одно", path, args[0])
		}
		path = args[0]
	}

	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("Ошибка получения пути, откуда запустили команду: %w", err)
		}
		path = wd
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("Ошибка получения абсолютного пути %q: %w", path, err)
	}

	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("Ошибка: путь к проекту %q недоступен: %w", abs, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("Ошибка: путь к проекту %q не является директорией", abs)
	}

	return abs, nil
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}

// branchFor возвращает ветку для remote: --force-branch, HEAD-ветку remote или --default-branch.
func (o *options) branchFor(warn io.Writer, root, remote string) string {
	if o.forceBranch != "" {
		return o.forceBranch
	}

	branch, err := detectHeadBranch(root, remote)
	if err == nil {
		return branch
	}

	if !o.silent {
		fmt.Fprintf(warn, "У репозитория %q неизвестна HEAD-ветка. Ставлю значение по-умолчанию: %q\n", remote, o.defaultBranch)
	}
	return o.defaultBranch
}

func (o *options) createBranchesFile(cmd *cobra.Command, args []string) error {
	root, err := resolveRoot(o.projectRoot, args)
	if err != nil {
		return err
	}

	fullFilePath := branchesPath(root)
	_, err = os.Stat(fullFilePath)
	if err == nil && !o.rewrite {
		return fmt.Errorf("Ошибка: Файл %q уже существует! Для перезаписи используйте -r", fullFilePath)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("Ошибка проверки %q: %w", fullFilePath, err)
	}

	remotes, err := listRemotes(root)
	if err != nil {
		return err
	}

	if len(remotes) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "В git репозитории %q нет установленных удаленных репозиториев. Просто выхожу...\n", root)
		return nil
	}

	targets := Targets{Targets: make([]Target, 0, len(remotes))}
	for _, remote := range remotes {
		targets.Add(Target{Remote: remote, Branch: o.branchFor(cmd.ErrOrStderr(), root, remote)})
	}

	return saveTargets(root, targets)
}

func newRootCmd() *cobra.Command {
	o := &options{}

	rootCmd := &cobra.Command{
		Use:   "brancher [path]",
		Short: "Создаёт и редактирует .branches для git-push агента",
		Long: "Консольная утилита для моего git-push агента. Запускается в проекте с git и создает базовую структуру .branches для всех remote.\n" +
			"Без подкоманды работает как `brancher init`.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          o.createBranchesFile,
	}

	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&o.projectRoot, "project", "p", "", "Путь к корню целевого проекта. По-умолчанию директория, откуда запустили утилиту")
	pf.StringVarP(&o.defaultBranch, "default-branch", "b", "master", "Название ветки, подставляемое, если HEAD-ветку remote определить не удалось.")
	pf.BoolVarP(&o.silent, "silent", "s", false, "Не выводить предупреждения.")

	addInitFlags(rootCmd, o)

	initCmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Создать .branches для всех remote (то же, что brancher без подкоманды)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  o.createBranchesFile,
	}
	addInitFlags(initCmd, o)

	rootCmd.AddCommand(
		initCmd,
		newSetCmd(o),
		newAddCmd(o),
		newRmCmd(o),
		newListCmd(o),
		newSyncCmd(o),
	)

	return rootCmd
}

func addForceBranchFlag(cmd *cobra.Command, o *options) {
	cmd.Flags().StringVarP(&o.forceBranch, "force-branch", "f", "", "Если установлен, будет ставить эту ветку, а не HEAD ветку репозитория")
}

func addInitFlags(cmd *cobra.Command, o *options) {
	addForceBranchFlag(cmd, o)
	cmd.Flags().BoolVarP(&o.rewrite, "rewrite", "r", false, "Перезаписать .branches-файл, если таковой будет найден")
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
