package generatorv3

import (
	"fmt"

	asyncapi "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/formats"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/generators"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/options"
)

// applyRegisteredFormats runs the Validate and Prepare hooks of the
// registered format matching each message's contentType.
func (g Generator) applyRegisteredFormats() error {
	for _, msg := range g.Specification.Components.Messages {
		if err := applyFormat(&g.Specification, msg); err != nil {
			return err
		}
	}

	for _, ch := range g.Specification.Channels {
		for _, msg := range ch.Follow().Messages {
			// ponytail: channel walk may revisit component messages; the
			// CloudEvents Prepare hook is idempotent
			if err := applyFormat(&g.Specification, msg); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyFormat(spec *asyncapi.Specification, msg *asyncapi.Message) error {
	f := formats.ForMessage(msg)
	if f == nil {
		return nil
	}

	if f.Validate != nil {
		if err := f.Validate(msg); err != nil {
			return err
		}
	}

	if f.Prepare != nil {
		return f.Prepare(spec, msg)
	}

	return nil
}

// Generator is the structure that contains information to generate the code from
// the specification.
type Generator struct {
	Options       options.Options
	Specification asyncapi.Specification
	ModulePath    string
	ModuleVersion string
}

// Generate generates the source code from the specification.
func (g Generator) Generate() (string, error) {
	if err := g.applyRegisteredFormats(); err != nil {
		return "", err
	}

	content, err := g.generateImports(g.Options)
	if err != nil {
		return "", err
	}

	// Generate the shared controller boilerplate once, only when a controller
	// side is generated. This keeps the `types` output free of it.
	base, err := g.generateControllerBase()
	if err != nil {
		return "", err
	}
	content += base

	for remainingParts, part := true, ""; remainingParts; part = "" {
		switch {
		case g.Options.Generate.Application:
			part, err = g.generateApp()
			g.Options.Generate.Application = false
		case g.Options.Generate.User:
			part, err = g.generateUser()
			g.Options.Generate.User = false
		case g.Options.Generate.Types:
			part, err = g.generateTypes()
			g.Options.Generate.Types = false
		default:
			remainingParts = false
		}

		if err != nil {
			return "", err
		}

		content += part
	}

	return content, nil
}

func (g Generator) generateImports(opts options.Options) (string, error) {
	imps, err := g.Specification.CustomImports()
	if err != nil {
		return "", fmt.Errorf("failed to generate custom imports: %w", err)
	}
	for _, imp := range formats.Imports() {
		imps = append(imps, fmt.Sprintf("%q", imp))
	}

	return ImportsGenerator{
		PackageName:   opts.PackageName,
		ModuleVersion: g.ModuleVersion,
		ModuleName:    g.ModulePath,
		CustomImports: imps,
	}.Generate()
}

func (g Generator) generateTypes() (string, error) {
	return TypesGenerator{Specification: g.Specification}.Generate()
}

// generateControllerBase returns the shared controller boilerplate, but only
// when an application or user controller is generated; otherwise it returns an
// empty string so that the `types` output stays free of it.
func (g Generator) generateControllerBase() (string, error) {
	if !g.Options.Generate.Application && !g.Options.Generate.User {
		return "", nil
	}

	return ControllerBaseGenerator{
		MultiMessageReceive: g.hasMultiMessageReceive(),
	}.Generate()
}

// hasMultiMessageReceive reports whether any of the controller sides that will
// be generated has a receive operation carrying several messages (issue #333).
func (g Generator) hasMultiMessageReceive() bool {
	sides := make([]generators.Side, 0, 2)
	if g.Options.Generate.Application {
		sides = append(sides, generators.SideIsApplication)
	}
	if g.Options.Generate.User {
		sides = append(sides, generators.SideIsUser)
	}

	for _, side := range sides {
		if NewActionOperations(side, g.Specification).HasMultiMessageReceive() {
			return true
		}
	}

	return false
}

func (g Generator) generateApp() (string, error) {
	var content string

	// Generate application listener
	listener, err := NewSubscriberGenerator(
		generators.SideIsApplication,
		g.Specification,
	).Generate()
	if err != nil {
		return "", err
	}
	content += listener

	// Generate application controller
	controller, err := NewControllerGenerator(
		generators.SideIsApplication,
		g.Specification,
	).Generate()
	if err != nil {
		return "", err
	}
	content += controller

	return content, nil
}

func (g Generator) generateUser() (string, error) {
	var content string

	// Generate user listener
	listener, err := NewSubscriberGenerator(
		generators.SideIsUser,
		g.Specification,
	).Generate()
	if err != nil {
		return "", err
	}
	content += listener
	// Generate user controller
	controller, err := NewControllerGenerator(
		generators.SideIsUser,
		g.Specification,
	).Generate()
	if err != nil {
		return "", err
	}
	content += controller

	return content, nil
}
