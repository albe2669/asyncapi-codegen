package formats_test

import (
	"strings"
	"testing"

	asyncapiv3 "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/formats"
)

func TestRegisterAndMatch(t *testing.T) {
	f := formats.Format{MediaType: " application/X-Custom+JSON ; charset=utf-8"}
	if err := formats.Register(f); err != nil {
		t.Fatal(err)
	}

	// Matching ignores case, spaces and parameters.
	for _, ct := range []string{
		"application/x-custom+json",
		"Application/X-Custom+JSON",
		"application/x-custom+json; v=2",
	} {
		if formats.Match(ct) == nil {
			t.Errorf("Match(%q) should find the custom format", ct)
		}
	}

	if formats.Match("application/json") != nil {
		t.Error("Match should return nil for an unregistered media type")
	}

	msg := asyncapiv3.Message{ContentType: "application/x-custom+json"}
	if formats.ForMessage(&msg) == nil {
		t.Error("ForMessage should follow the message contentType")
	}

	referencing := asyncapiv3.Message{ReferenceTo: &msg}
	if formats.ForMessage(&referencing) == nil {
		t.Error("ForMessage should follow references")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	err := formats.Register(formats.Format{MediaType: "application/x-dup"})
	if err != nil {
		t.Fatal(err)
	}

	err = formats.Register(formats.Format{MediaType: "application/x-dup"})
	if err == nil {
		t.Fatal("Register should reject a duplicate media type")
	}
}

func TestRegisterEmptyMediaType(t *testing.T) {
	if err := formats.Register(formats.Format{MediaType: "  "}); err == nil {
		t.Fatal("Register should reject an empty media type")
	}
}

func TestExecuteSnippet(t *testing.T) {
	f := formats.Format{MediaType: "application/x-snippet"}
	f.Snippets.StructFields = "value: {{.Value}}"
	if err := formats.Register(f); err != nil {
		t.Fatal(err)
	}

	out, err := formats.ExecuteSnippet(formats.Match("application/x-snippet"), f.Snippets.Select("StructFields"),
		struct{ Value string }{"x"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(out) != "value: x" {
		t.Errorf("unexpected snippet output %q", out)
	}
}

func TestSnippetsSelect(t *testing.T) {
	s := formats.Snippets{
		StructFields:    "a",
		ConstructorBody: "b",
		BrokerToMessage: "c",
		MessageToBroker: "d",
	}

	for hook, want := range map[string]string{
		"StructFields":    "a",
		"ConstructorBody": "b",
		"BrokerToMessage": "c",
		"MessageToBroker": "d",
		"Unknown":         "",
	} {
		if got := s.Select(hook); got != want {
			t.Errorf("Select(%q) = %q, want %q", hook, got, want)
		}
	}
}
