package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// validateRemoteName отсекает пустые имена и имена, похожие на флаги git.
func validateRemoteName(remote string) error {
	if strings.TrimSpace(remote) == "" {
		return errors.New("Ошибка: имя remote не может быть пустым")
	}
	if strings.HasPrefix(remote, "-") {
		return fmt.Errorf("Ошибка: некорректное имя remote %q", remote)
	}
	return nil
}

func validateBranchName(branch string) error {
	if strings.TrimSpace(branch) == "" {
		return errors.New("Ошибка: имя ветки не может быть пустым")
	}
	return nil
}

// loadExisting читает .branches и возвращает ошибку, если файла нет.
func loadExisting(root string) (Targets, error) {
	targets, exists, err := loadTargets(root)
	if err != nil {
		return Targets{}, err
	}
	if !exists {
		return Targets{}, fmt.Errorf("Ошибка: файл %q не найден. Создайте его через brancher init, brancher sync или brancher add", branchesPath(root))
	}
	return targets, nil
}

func newSetCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "set <remote> <branch>",
		Short: "Сменить ветку у существующей цели в .branches",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote, branch := args[0], args[1]
			if err := validateRemoteName(remote); err != nil {
				return err
			}
			if err := validateBranchName(branch); err != nil {
				return err
			}

			root, err := resolveRoot(o.projectRoot, nil)
			if err != nil {
				return err
			}
			targets, err := loadExisting(root)
			if err != nil {
				return err
			}

			i := targets.Index(remote)
			if i < 0 {
				return fmt.Errorf("Ошибка: remote %q нет в .branches. Добавьте его через brancher add %s или brancher sync", remote, remote)
			}
			old := targets.Targets[i].Branch
			targets.Targets[i].Branch = branch

			if err := saveTargets(root, targets); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Цель %q: ветка %q -> %q\n", remote, old, branch)
			return nil
		},
	}
}

func newAddCmd(o *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <remote> [url] [branch]",
		Short: "Добавить цель в .branches (и remote в git, если указан url)",
		Long: "Добавляет цель в .branches. Если указан url, сначала выполняет git remote add <remote> <url>.\n" +
			"Ветка: аргумент [branch], иначе -f, иначе HEAD-ветка remote, иначе --default-branch.\n" +
			"Чтобы указать ветку для уже существующего remote без url, используйте -f: brancher add origin -f main",
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote := args[0]
			if err := validateRemoteName(remote); err != nil {
				return err
			}
			var url, branch string
			if len(args) > 1 {
				url = strings.TrimSpace(args[1])
			}
			if len(args) > 2 {
				branch = args[2]
				if err := validateBranchName(branch); err != nil {
					return err
				}
			}

			root, err := resolveRoot(o.projectRoot, nil)
			if err != nil {
				return err
			}
			targets, _, err := loadTargets(root)
			if err != nil {
				return err
			}
			if targets.Index(remote) >= 0 {
				return fmt.Errorf("Ошибка: цель %q уже есть в .branches. Чтобы сменить ветку, используйте brancher set %s <branch>", remote, remote)
			}

			remotes, err := listRemotes(root)
			if err != nil {
				return err
			}
			inGit := slices.Contains(remotes, remote)

			if url != "" {
				if inGit {
					return fmt.Errorf("Ошибка: remote %q уже существует в git. Уберите url, чтобы просто добавить цель (ветку можно задать через -f)", remote)
				}
				if _, err := runGit(root, "remote", "add", "--", remote, url); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Добавлен git remote %q: %s\n", remote, url)
			} else if !inGit {
				return fmt.Errorf("Ошибка: remote %q не найден в git. Укажите url: brancher add %s <url>", remote, remote)
			}

			if branch == "" {
				branch = o.branchFor(cmd.ErrOrStderr(), root, remote)
			}

			targets.Add(Target{Remote: remote, Branch: branch})
			if err := saveTargets(root, targets); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Добавлена цель: %s -> %s\n", remote, branch)
			return nil
		},
	}
	addForceBranchFlag(cmd, o)
	return cmd
}

func newRmCmd(o *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rm <remote>",
		Aliases: []string{"remove"},
		Short:   "Удалить цель из .branches (с --git — ещё и remote из git)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote := args[0]
			if err := validateRemoteName(remote); err != nil {
				return err
			}

			root, err := resolveRoot(o.projectRoot, nil)
			if err != nil {
				return err
			}
			targets, err := loadExisting(root)
			if err != nil {
				return err
			}
			if !targets.Remove(remote) {
				return fmt.Errorf("Ошибка: цели %q нет в .branches", remote)
			}

			if o.removeGit {
				if _, err := runGit(root, "remote", "remove", remote); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Удалён git remote %q\n", remote)
			}

			if err := saveTargets(root, targets); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Удалена цель %q\n", remote)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&o.removeGit, "git", "g", false, "Также выполнить git remote remove <remote>")
	return cmd
}

func newListCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:     "list [path]",
		Aliases: []string{"ls"},
		Short:   "Показать цели из .branches и сверить их с git remote",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(o.projectRoot, args)
			if err != nil {
				return err
			}
			targets, exists, err := loadTargets(root)
			if err != nil {
				return err
			}
			remotes, err := listRemotes(root)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if !exists {
				fmt.Fprintf(out, "Файл %q не найден\n", branchesPath(root))
			} else if len(targets.Targets) == 0 {
				fmt.Fprintln(out, "В .branches нет целей")
			} else {
				fmt.Fprintln(out, "Цели в .branches:")
				for _, t := range targets.Targets {
					mark := ""
					if !slices.Contains(remotes, t.Remote) {
						mark = "  [нет такого remote в git]"
					}
					fmt.Fprintf(out, "  %s -> %s%s\n", t.Remote, t.Branch, mark)
				}
			}

			var missing []string
			for _, r := range remotes {
				if targets.Index(r) < 0 {
					missing = append(missing, r)
				}
			}
			if len(missing) > 0 {
				fmt.Fprintln(out, "Git remote без цели в .branches (добавьте через brancher sync или brancher add):")
				for _, r := range missing {
					fmt.Fprintf(out, "  %s  [нет в .branches]\n", r)
				}
			}
			return nil
		},
	}
}

func newSyncCmd(o *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [path]",
		Short: "Добавить в .branches цели для git remote, которых там ещё нет (существующие не меняются)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(o.projectRoot, args)
			if err != nil {
				return err
			}
			targets, exists, err := loadTargets(root)
			if err != nil {
				return err
			}
			remotes, err := listRemotes(root)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(remotes) == 0 && !exists {
				fmt.Fprintf(out, "В git репозитории %q нет установленных удаленных репозиториев. Просто выхожу...\n", root)
				return nil
			}

			var added []Target
			for _, r := range remotes {
				if targets.Index(r) >= 0 {
					continue
				}
				t := Target{Remote: r, Branch: o.branchFor(cmd.ErrOrStderr(), root, r)}
				targets.Add(t)
				added = append(added, t)
			}

			if len(added) == 0 {
				fmt.Fprintln(out, "Все git remote уже есть в .branches, ничего не изменено")
				return nil
			}

			if err := saveTargets(root, targets); err != nil {
				return err
			}
			for _, t := range added {
				fmt.Fprintf(out, "Добавлена цель: %s -> %s\n", t.Remote, t.Branch)
			}
			return nil
		},
	}
	addForceBranchFlag(cmd, o)
	return cmd
}
