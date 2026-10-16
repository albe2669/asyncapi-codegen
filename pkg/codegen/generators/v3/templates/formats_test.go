package templates

import (
	"testing"

	asyncapiv3 "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/formats"
)

func TestMatchesCloudEvents(t *testing.T) {
	cases := []struct {
		Name        string
		ContentType string
		Result      bool
	}{
		{"Exact match", "application/cloudevents+json", true},
		{"Extra parameters", "application/cloudevents+json; charset=utf-8", true},
		{"Version parameter", "application/cloudevents+json; version=0.3", true},
		{"Case insensitive", "Application/CloudEvents+JSON", true},
		{"Surrounding spaces", "  application/cloudevents+json  ", true},
		{"Different subtype", "application/json", false},
		{"Different type", "text/cloudevents+json", false},
		{"Empty", "", false},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg := asyncapiv3.Message{ContentType: c.ContentType}
			if got := formats.ForMessage(&msg) != nil; got != c.Result {
				t.Errorf("ForMessage(%q) matched = %v, want %v", c.ContentType, got, c.Result)
			}
		})
	}
	t.Run("Follow reference", func(t *testing.T) {
		referenced := asyncapiv3.Message{ContentType: "application/cloudevents+json"}
		msg := asyncapiv3.Message{ReferenceTo: &referenced}
		if formats.ForMessage(&msg) == nil {
			t.Error("ForMessage should follow references")
		}
	})

	t.Run("Nil message", func(t *testing.T) {
		if formats.ForMessage(nil) != nil {
			t.Error("ForMessage(nil) should be nil")
		}
	})
}

func TestCloudEventSpecVersion(t *testing.T) {
	cases := []struct {
		Name        string
		ContentType string
		Result      string
	}{
		{Name: "No parameter", ContentType: "application/cloudevents+json", Result: "1.0"},
		{Name: "Version 1.0", ContentType: "application/cloudevents+json; version=1.0", Result: "1.0"},
		{Name: "Version 0.3", ContentType: "application/cloudevents+json; version=0.3", Result: "0.3"},
		{Name: "Spaced version", ContentType: "application/cloudevents+json ;  version = 0.3 ", Result: "0.3"},
		{Name: "Unsupported version", ContentType: "application/cloudevents+json; version=2.0", Result: "2.0"},
		{Name: "Other parameter", ContentType: "application/cloudevents+json; charset=utf-8", Result: "1.0"},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg := asyncapiv3.Message{ContentType: c.ContentType}
			if got := CloudEventSpecVersion(&msg); got != c.Result {
				t.Errorf("CloudEventSpecVersion(%q) = %q, want %q", c.ContentType, got, c.Result)
			}
		})
	}

	t.Run("Follow reference", func(t *testing.T) {
		referenced := asyncapiv3.Message{ContentType: "application/cloudevents+json; version=0.3"}
		msg := asyncapiv3.Message{ReferenceTo: &referenced}
		if got := CloudEventSpecVersion(&msg); got != "0.3" {
			t.Errorf("CloudEventSpecVersion should follow references, got %q", got)
		}
	})

	t.Run("Nil message", func(t *testing.T) {
		if got := CloudEventSpecVersion(nil); got != "1.0" {
			t.Errorf("CloudEventSpecVersion(nil) = %q, want 1.0", got)
		}
	})

	t.Run("Non CloudEvents content type", func(t *testing.T) {
		msg := asyncapiv3.Message{ContentType: "application/json"}
		if got := CloudEventSpecVersion(&msg); got != "1.0" {
			t.Errorf("CloudEventSpecVersion should default to 1.0, got %q", got)
		}
	})
}

func TestCloudEventsValidate(t *testing.T) {
	cases := []struct {
		Name        string
		ContentType string
		WantError   bool
	}{
		{"Default", "application/cloudevents+json", false},
		{"Version 1.0", "application/cloudevents+json; version=1.0", false},
		{"Version 0.3", "application/cloudevents+json; version=0.3", false},
		{"Version 2.0", "application/cloudevents+json; version=2.0", true},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			msg := asyncapiv3.Message{Name: "Test", ContentType: c.ContentType}
			err := validateCloudEventMessage(&msg)
			if c.WantError && err == nil {
				t.Error("expected an error")
			}
			if !c.WantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
