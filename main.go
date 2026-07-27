package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

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

var (
	defaultBranch string
	projectRoot   string
	forceBranch   string
	silent        bool
	rewrite       bool

	currentPath string
)

func fetchOutAndErr(c *exec.Cmd) ([]byte, []byte, error) {
	stdout, outGetError := c.StdoutPipe()
	if outGetError != nil {
		return nil, nil, outGetError
	}

	stderr, errGetError := c.StderrPipe()
	if errGetError != nil {
		return nil, nil, errGetError
	}

	c.Start()

	outBytes, readErr := io.ReadAll(stdout)
	if readErr != nil {
		return nil, nil, readErr
	}

	errBytes, readErr := io.ReadAll(stderr)
	if readErr != nil {
		return nil, nil, readErr
	}

	c.Wait()

	return outBytes, errBytes, nil
}

var rootCmd = &cobra.Command{
	Use:  "brancher [path]",
	Long: "Консольная утилита для моего git-push агента. Запускается в проекте с git и создает базовую структуру .branches для всех remote",

	RunE: createBrancheFile,
}

func createBrancheFile(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("Ожидается максимум 1 позиционный аргумент")
	}

	if projectRoot == "" {
		if len(args) == 1 {
			projectRoot = args[0]
		} else {
			projectRoot = currentPath
		}
	}

	fullFilePath := currentPath + "/.branches"
	_, err := os.Stat(fullFilePath)

	// Файл уже существует
	if err == nil && !rewrite {
		return fmt.Errorf("Ошибка: Файл %q уже существует!", fullFilePath)
	}

	command := exec.Command("git", "remote", "show")
	command.Dir = currentPath

	outBytes, errBytes, comError := fetchOutAndErr(command)
	if comError != nil {
		return comError
	}

	if len(errBytes) != 0 {
		return errors.New(string(errBytes))
	}

	rawRemotes := strings.Split(string(outBytes), "\n")
	if len(rawRemotes) == 0 {
		fmt.Printf("В текущем git репозитории (%q) нет установленных удаленных репозиториев. Просто выхожу...", currentPath)
		return nil
	}
	remotes := rawRemotes[:len(rawRemotes)-1]

	targets := Targets{Targets: make([]Target, 0, len(remotes))}

	if forceBranch == "" {
		for _, remoteDir := range remotes {
			remoteDir = strings.Trim(remoteDir, "\n")

			subcommand := fmt.Sprintf("git remote show %s | grep HEAD", remoteDir)
			command = exec.Command("sh", "-c", subcommand)
			command.Dir = currentPath

			outBytes, errBytes, gitErr := fetchOutAndErr(command)
			if gitErr != nil {
				return gitErr
			}

			if len(errBytes) != 0 {
				return errors.New(string(errBytes))
			}

			splitted := strings.Split(string(outBytes), " ")
			gitBranch := strings.Trim(splitted[len(splitted)-1], "\n") // HEAD branch: {branch_name}

			var branch string
			if gitBranch == "(uknown)" || gitBranch == "(неизвестно)" {
				if !silent {
					fmt.Printf("У репозитория %q неизвестна HEAD-ветка. Ставлю значение по-умолчанию: %q \n", remoteDir, defaultBranch)
				}
				branch = defaultBranch
			} else {
				branch = gitBranch
			}

			targets.Add(Target{Branch: branch, Remote: remoteDir})
		}
	} else {
		for _, remoteDir := range remotes {
			targets.Add(Target{Remote: strings.Trim(remoteDir, "\n"), Branch: forceBranch})
		}
	}

	branchFile, creatingErr := os.Create(fullFilePath)
	if creatingErr != nil {
		return creatingErr
	}
	defer branchFile.Close()

	sered, serErr := json.Marshal(targets)
	if serErr != nil {
		return fmt.Errorf("Ошибка на этапе сериализации: %s", serErr.Error())
	}

	branchFile.Write(sered)

	return nil
}

func init() {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Println("Ошибка получения пути, откуда запустили команду: ", err.Error())
		os.Exit(1)
	}
	currentPath = wd

	rootCmd.Flags().StringVarP(&defaultBranch, "default-branch", "b", "master", "Название ветки, подставляемое в итоговый шаблон по-умолчанию. По-умолчанию master")
	rootCmd.Flags().StringVarP(&projectRoot, "project", "p", "", "Путь к корню целевого проекта. По-умолчанию директория, откуда запустили утилиту")
	rootCmd.Flags().StringVarP(&forceBranch, "force-branch", "f", "", "Если установлен, будет выбирать ставить эту ветку, а не HEAD ветку репозитория")
	rootCmd.Flags().BoolVarP(&silent, "silent", "s", false, "Не выводить предупреждения.")
	rootCmd.Flags().BoolVarP(&rewrite, "rewrite", "r", false, "Перезаписать .branches-файл, если таковой будет найден")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
