package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/albe2669/asyncapi-codegen/pkg/codegen/options"
	"github.com/spf13/cobra"
)

// ErrInvalidGenerate happens when using an invalid generation argument.
var ErrInvalidGenerate = errors.New("invalid generate argument")

// Flags contains all command line flags.
type Flags struct {
	// InputPaths are the path of the AsyncAPI specification file and its dependencies
	InputPaths []string

	// OutputPath is the path of the generated code file
	OutputPath string

	// PackageName is the package name of the generated code
	PackageName string

	// Generate contains options, separated by commas, regarding which
	// golang code should be generated
	Generate string

	// DisableFormatting states if the formatting should be disabled when
	// writing the generated code
	DisableFormatting bool

	// ConvertKeys defines a schema property keys conversion strategy.
	// Supported values: snake, camel, pascal, kebab, none
	ConvertKeys string

	// NamingScheme defines the naming case for generated golang structs
	// Supported values: camel, pascal, none
	NamingScheme string

	// IgnoreStringFormat states whether the properties' format (date, date-time) should impact the type in types
	IgnoreStringFormat bool

	// ForcePointers can be used to force all struct fields to be generated as pointers
	ForcePointers bool

	// BreakingAgainst is the buf breaking target for protobuf CloudEvents
	// messages: a buf ref (e.g. '.git#branch=main'), a local path or any
	// 'buf breaking --against' target. Empty means the last generated output
	// is the baseline.
	BreakingAgainst string
}

// SetToCommand adds the flags to a cobra command.
func (f *Flags) SetToCommand(cmd *cobra.Command) {
	cmd.Flags().StringSliceVarP(
		&f.InputPaths, "input", "i", []string{"asyncapi.yaml"},
		"AsyncAPI specification file to use, and its dependencies")
	cmd.Flags().StringVarP(&f.OutputPath, "output", "o", "asyncapi.gen.go", "Destination file")
	cmd.Flags().StringVarP(&f.PackageName, "package", "p", "asyncapi", "Golang package name")
	cmd.Flags().StringVarP(&f.Generate, "generate", "g", "user,application,types", "Generation options")
	cmd.Flags().BoolVarP(&f.DisableFormatting, "disable-formatting", "f", false, "Disables the code generation formatting")
	cmd.Flags().StringVarP(&f.ConvertKeys, "convert-keys", "c", "none",
		"Schema property key names conversion strategy.\nSupported values: snake, camel, pascal, kebab, none.")
	cmd.Flags().StringVarP(&f.NamingScheme, "naming-scheme", "n", "none",
		"Naming scheme for generated golang elements.\nSupported values: camel, pascal, none.")
	cmd.Flags().BoolVar(&f.IgnoreStringFormat, "ignore-string-format", false,
		"Ignores the format (date, date-time) on string properties, generating golang string, instead of dates")
	cmd.Flags().BoolVar(&f.ForcePointers, "force-pointers", false,
		"Forces all struct fields to be generated as pointers")
	cmd.Flags().StringVar(&f.BreakingAgainst, "breaking-against", "",
		"Buf breaking check target for application/cloudevents+protobuf messages.\n"+
			"Accepts a buf ref (e.g. '.git#branch=main'), a local path or any\n"+
			"'buf breaking --against' target. Default: use the last generated\n"+
			"output as the baseline.")
}

// ToCodegenOptions processes command line flags structure to code generation tool options.
func (f Flags) ToCodegenOptions() (options.Options, error) {
	opt := options.Options{
		OutputPath:         f.OutputPath,
		PackageName:        f.PackageName,
		DisableFormatting:  f.DisableFormatting,
		ConvertKeys:        f.ConvertKeys,
		NamingScheme:       f.NamingScheme,
		IgnoreStringFormat: f.IgnoreStringFormat,
		ForcePointers:      f.ForcePointers,
		BreakingAgainst:    f.BreakingAgainst,
	}

	if f.Generate != "" {
		gens := strings.Split(f.Generate, ",")
		for _, v := range gens {
			switch v {
			case "application":
				opt.Generate.Application = true
			case "user":
				opt.Generate.User = true
			case "types":
				opt.Generate.Types = true
			default:
				return opt, fmt.Errorf("%w: %q", ErrInvalidGenerate, v)
			}
		}
	}

	return opt, nil
}
