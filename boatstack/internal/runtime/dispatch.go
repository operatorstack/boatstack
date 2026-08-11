package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ShouldDispatch(argv0 string) bool {
	name := strings.ToLower(filepath.Base(argv0))
	return name == "boatstack" || name == "boatstack.exe"
}

func Dispatch(arguments []string) (int, error) {
	repository, err := repositoryArgument(arguments)
	if err != nil {
		return 1, err
	}
	executable, _, err := ResolvePinnedExecutable(repository)
	if err != nil {
		return 1, err
	}
	return execute(executable, append([]string{executable}, arguments...), os.Environ())
}

func ResolvePinnedExecutable(repository string) (string, Pin, error) {
	repository, err := findPinnedRepository(repository)
	if err != nil {
		return "", Pin{}, err
	}
	raw, err := os.ReadFile(PinPath(repository))
	if err != nil {
		return "", Pin{}, fmt.Errorf("read repository runtime pin: %w", err)
	}
	pin, err := DecodePin(raw)
	if err != nil {
		return "", Pin{}, fmt.Errorf("decode repository runtime pin: %w", err)
	}
	home, err := Home("")
	if err != nil {
		return "", Pin{}, err
	}
	executable, err := ExecutablePath(home, pin.Identity())
	if err != nil {
		return "", Pin{}, err
	}
	if err := VerifyExecutable(executable, pin.Identity()); err != nil {
		return "", Pin{}, err
	}
	return executable, pin, nil
}

func repositoryArgument(arguments []string) (string, error) {
	repository := ""
	for index := 0; index < len(arguments); index++ {
		value := arguments[index]
		if value == "--repo" {
			if index+1 >= len(arguments) || strings.TrimSpace(arguments[index+1]) == "" {
				return "", fmt.Errorf("--repo requires a path")
			}
			repository = arguments[index+1]
			index++
			continue
		}
		if strings.HasPrefix(value, "--repo=") {
			repository = strings.TrimPrefix(value, "--repo=")
		}
	}
	if repository == "" {
		var err error
		repository, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(repository)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func findPinnedRepository(start string) (string, error) {
	current := start
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		if _, err := os.Stat(PinPath(current)); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return "", fmt.Errorf("repository at %s has no Boatstack runtime pin; initialize it first", current)
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no Boatstack runtime pin found from %s; initialize this repository first", start)
		}
		current = parent
	}
}
