package app

import (
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandDocument is a stable, execution-free description of one visible CLI
// command. It exists so the product documentation can be generated from the
// same Cobra tree users execute instead of maintaining a second command list.
type CommandDocument struct {
	Path        string         `json:"path"`
	Use         string         `json:"use"`
	Summary     string         `json:"summary"`
	Description string         `json:"description,omitempty"`
	Example     string         `json:"example,omitempty"`
	Flags       []FlagDocument `json:"flags"`
}

// FlagDocument describes one local, persistent, or inherited command flag.
type FlagDocument struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Usage     string `json:"usage"`
	Default   string `json:"default"`
	Type      string `json:"type"`
}

// CommandDocumentation returns the visible CLI surface without executing
// pre-run hooks, opening a project, or starting a daemon.
func CommandDocumentation() []CommandDocument {
	commandLine := &cli{out: io.Discard, err: io.Discard, timeout: 10 * time.Second}
	root := commandLine.root()
	documents := make([]CommandDocument, 0)
	collectCommandDocumentation(root, &documents)
	sort.Slice(documents, func(left, right int) bool {
		return documents[left].Path < documents[right].Path
	})
	return documents
}

func collectCommandDocumentation(command *cobra.Command, documents *[]CommandDocument) {
	if command.Hidden {
		return
	}
	document := CommandDocument{
		Path:        command.CommandPath(),
		Use:         command.UseLine(),
		Summary:     command.Short,
		Description: command.Long,
		Example:     command.Example,
		Flags:       documentedFlags(command),
	}
	*documents = append(*documents, document)
	children := command.Commands()
	sort.Slice(children, func(left, right int) bool {
		return children[left].Name() < children[right].Name()
	})
	for _, child := range children {
		collectCommandDocumentation(child, documents)
	}
}

func documentedFlags(command *cobra.Command) []FlagDocument {
	flagsByName := make(map[string]FlagDocument)
	addFlags := func(flagSet *pflag.FlagSet) {
		flagSet.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			flagsByName[flag.Name] = FlagDocument{
				Name: flag.Name, Shorthand: flag.Shorthand, Usage: flag.Usage,
				Default: flag.DefValue, Type: flag.Value.Type(),
			}
		})
	}
	addFlags(command.NonInheritedFlags())
	addFlags(command.InheritedFlags())
	flags := make([]FlagDocument, 0, len(flagsByName))
	for _, flag := range flagsByName {
		flags = append(flags, flag)
	}
	sort.Slice(flags, func(left, right int) bool {
		return flags[left].Name < flags[right].Name
	})
	return flags
}
