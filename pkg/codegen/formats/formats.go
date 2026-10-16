// Package formats is a registry of payload wire formats that generated
// messages can use instead of the default JSON encoding.
//
// A format matches messages by the media type of their AsyncAPI contentType
// (the part before the first ';'). Each format contributes template snippets
// that the v3 generator injects at fixed hook points of the generated code
// (struct fields, constructor body, broker conversions and imports), plus
// optional hooks to validate a message and to prepare spec-level defaults.
//
// The CloudEvents format ships built-in; library users can register their
// own formats with Register before running code generation.
package formats

import (
	"fmt"
	"strings"
	"text/template"

	asyncapiv3 "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
)

// Snippets holds the Go template snippets a format injects into the
// generated code. Each snippet is executed with the AsyncAPI message as
// dot, and with the generator helper functions plus the format Funcs
// available.
type Snippets struct {
	// StructFields is inserted inside the generated message struct, before
	// the headers and payload fields. It is typically an embedded envelope.
	StructFields string
	// ConstructorBody is inserted inside the generated NewXMessage
	// constructor, before the correlation ID block.
	ConstructorBody string
	// BrokerToMessage replaces the default JSON payload unmarshal in the
	// generated brokerMessageToX function. It must fill msg from
	// bMsg.Payload and return msg, err on failure. Headers are handled by
	// the generator outside of this snippet.
	BrokerToMessage string
	// MessageToBroker replaces the default payload marshal in the generated
	// toBrokerMessage function. It must set the payload variable ([]byte)
	// and return extensions.BrokerMessage{}, err on failure. Headers are
	// handled by the generator outside of this snippet.
	MessageToBroker string
}

// Select returns the snippet for the given hook point name, or an empty
// string when the format does not provide it.
func (s Snippets) Select(hook string) string {
	switch hook {
	case "StructFields":
		return s.StructFields
	case "ConstructorBody":
		return s.ConstructorBody
	case "BrokerToMessage":
		return s.BrokerToMessage
	case "MessageToBroker":
		return s.MessageToBroker
	default:
		return ""
	}
}

// Format describes a payload wire format for v3 code generation.
type Format struct {
	// MediaType matches the contentType of the messages using this format.
	// Matching ignores case, surrounding spaces and any parameters after
	// the first ';'.
	MediaType string

	// Imports are Go import paths that the snippets may need. Unused
	// imports are removed by the formatter.
	Imports []string

	// Funcs are additional template functions available to the snippets.
	Funcs template.FuncMap

	// Snippets injected into the generated messages.
	Snippets Snippets

	// Validate is called once per message using this format before
	// generation. It must return an error to abort generation, typically
	// when the contentType declares an unsupported variant of the format.
	Validate func(msg *asyncapiv3.Message) error

	// Prepare is called once per message using this format before
	// generation, after Validate. It can set spec-level defaults on the
	// message.
	Prepare func(spec *asyncapiv3.Specification, msg *asyncapiv3.Message) error
}

// registeredFormats maps media types to formats.
var registeredFormats = map[string]*Format{}

// Register adds a format to the registry. It returns an error if the media
// type is invalid or already registered. Library users must call Register
// before running code generation.
func Register(f Format) error {
	mediaType := normalizeMediaType(f.MediaType)
	if mediaType == "" {
		return fmt.Errorf("format media type must not be empty")
	}

	if _, exists := registeredFormats[mediaType]; exists {
		return fmt.Errorf("format %q is already registered", mediaType)
	}

	registeredFormats[mediaType] = &f

	return nil
}

// Match returns the registered format for a contentType, or nil when the
// media type matches no format, which selects the default JSON encoding.
func Match(contentType string) *Format {
	return registeredFormats[normalizeMediaType(contentType)]
}

// ForMessage returns the registered format for a message, following
// references, or nil.
func ForMessage(msg *asyncapiv3.Message) *Format {
	if msg == nil {
		return nil
	}

	return Match(msg.Follow().ContentType)
}

// All returns every registered format.
func All() []*Format {
	all := make([]*Format, 0, len(registeredFormats))

	for _, f := range registeredFormats {
		all = append(all, f)
	}

	return all
}

// Imports returns the Go imports of every registered format.
func Imports() []string {
	var imports []string

	for _, f := range registeredFormats {
		imports = append(imports, f.Imports...)
	}

	return imports
}

// ExecuteSnippet parses and executes a format snippet with the given dot and
// function map. Snippets are small and parsed on demand.
func ExecuteSnippet(f *Format, snippet string, dot any, funcs template.FuncMap) (string, error) {
	tmplt, err := template.New("format-snippet").Funcs(funcs).Parse(snippet)
	if err != nil {
		return "", fmt.Errorf("failed to parse format %q snippet: %w", f.MediaType, err)
	}

	buf := new(strings.Builder)
	if err := tmplt.Execute(buf, dot); err != nil {
		return "", fmt.Errorf("failed to execute format %q snippet: %w", f.MediaType, err)
	}

	return buf.String(), nil
}

// normalizeMediaType extracts the lowercased media type from a contentType,
// ignoring parameters and surrounding spaces.
func normalizeMediaType(contentType string) string {
	first := strings.TrimSpace(strings.Split(contentType, ";")[0])
	return strings.ToLower(first)
}
