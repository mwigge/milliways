package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/config"
	"gopkg.in/yaml.v3"
)

var argumentPattern = regexp.MustCompile(`\$([A-Z][A-Z0-9_]*)`)

// Argument describes one named command argument.
type Argument struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// Command stores one slash command definition.
type Command struct {
	Name        string
	Description string
	Arguments   []Argument
	Content     string
	Namespace   string
}

type commandFrontmatter struct {
	Name        string     `yaml:"name"`
	Namespace   string     `yaml:"namespace"`
	Description string     `yaml:"description"`
	Arguments   []Argument `yaml:"arguments"`
}

// LoadCommands loads markdown command definitions from dir.
func LoadCommands(dir string) ([]Command, error) {
	return loadCommandsWithNamespace(dir, "user")
}

func loadCommandsWithNamespace(dir string, defaultNamespace string) ([]Command, error) {
	if err := config.GuardReadPath(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read commands dir %q: %w", dir, err)
	}

	commands := make([]Command, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			nested, err := loadCommandsWithNamespace(filepath.Join(dir, entry.Name()), defaultNamespace)
			if err != nil {
				return nil, err
			}
			commands = append(commands, nested...)
			continue
		}
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		commandPath := filepath.Join(dir, entry.Name())
		command, err := loadCommandFile(commandPath, defaultNamespace)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands, nil
}

func loadCommandFile(path string, defaultNamespace string) (Command, error) {
	if err := config.GuardReadPath(path); err != nil {
		return Command{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Command{}, fmt.Errorf("read command %q: %w", path, err)
	}

	frontmatter, body, err := parseCommandDocument(string(data))
	if err != nil {
		return Command{}, fmt.Errorf("parse command %q: %w", path, err)
	}

	name := strings.TrimSpace(frontmatter.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	namespace := strings.TrimSpace(frontmatter.Namespace)
	if namespace == "" {
		namespace = defaultNamespace
	}

	arguments := mergeArguments(frontmatter.Arguments, body)

	return Command{
		Name:        name,
		Description: strings.TrimSpace(frontmatter.Description),
		Arguments:   arguments,
		Content:     body,
		Namespace:   namespace,
	}, nil
}

func parseCommandDocument(content string) (commandFrontmatter, string, error) {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return commandFrontmatter{}, strings.TrimSpace(trimmed), nil
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	separator := "\n---\n"
	index := strings.Index(rest, separator)
	if index < 0 {
		return commandFrontmatter{}, "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatterText := rest[:index]
	body := rest[index+len(separator):]

	var frontmatter commandFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterText), &frontmatter); err != nil {
		return commandFrontmatter{}, "", fmt.Errorf("decode frontmatter: %w", err)
	}

	return frontmatter, strings.TrimSpace(body), nil
}

func mergeArguments(frontmatterArgs []Argument, body string) []Argument {
	result := make([]Argument, 0, len(frontmatterArgs))
	seen := make(map[string]struct{}, len(frontmatterArgs))

	for _, argument := range frontmatterArgs {
		argument.Name = strings.TrimSpace(argument.Name)
		if argument.Name == "" {
			continue
		}
		if !argument.Required {
			argument.Required = false
		}
		result = append(result, argument)
		seen[argument.Name] = struct{}{}
	}

	matches := argumentPattern.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		name := match[1]
		if _, ok := seen[name]; ok {
			continue
		}
		result = append(result, Argument{Name: name, Required: true})
		seen[name] = struct{}{}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}
