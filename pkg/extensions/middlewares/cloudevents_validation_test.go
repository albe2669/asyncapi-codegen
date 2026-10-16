package middlewares_test

import (
	"context"
	"testing"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/go-playground/validator/v10"
	"github.com/albe2669/asyncapi-codegen/pkg/extensions"
	"github.com/albe2669/asyncapi-codegen/pkg/extensions/middlewares"
	"github.com/stretchr/testify/require"
)

type cloudeventsPayload struct {
	Name string `json:"name" validate:"required"`
}

func validEnvelope(t *testing.T, data string) []byte {
	t.Helper()

	ev := event.New()
	ev.SetID("42")
	ev.SetSource("test")
	ev.SetType("test.event")
	require.NoError(t, ev.SetData(event.ApplicationJSON, []byte(data)))

	raw, err := ev.MarshalJSON()
	require.NoError(t, err)

	return raw
}

func TestCloudEventsValidationAllowsValidEvent(t *testing.T) {
	mw := middlewares.CloudEventsValidation(validator.New(), func() any { return &cloudeventsPayload{} })

	called := false
	next := func(_ context.Context) error {
		called = true
		return nil
	}

	msg := &extensions.BrokerMessage{Payload: validEnvelope(t, `{"name":"alice"}`)}
	err := mw(context.Background(), msg, next)

	require.NoError(t, err)
	require.True(t, called, "next middleware must be called for a valid event")
}

func TestCloudEventsValidationRejectsInvalidPayload(t *testing.T) {
	mw := middlewares.CloudEventsValidation(validator.New(), func() any { return &cloudeventsPayload{} })

	called := false
	next := func(_ context.Context) error {
		called = true
		return nil
	}

	// Missing the required "name" field in the event data.
	msg := &extensions.BrokerMessage{Payload: validEnvelope(t, `{}`)}
	err := mw(context.Background(), msg, next)

	require.Error(t, err)
	require.False(t, called, "next middleware must not be called for an invalid payload")
}

func TestCloudEventsValidationRejectsInvalidEnvelope(t *testing.T) {
	mw := middlewares.CloudEventsValidation(validator.New(), func() any { return &cloudeventsPayload{} })

	called := false
	next := func(_ context.Context) error {
		called = true
		return nil
	}

	// Valid JSON object, but not a CloudEvents envelope: missing the
	// required attributes, so the envelope validation must fail.
	msg := &extensions.BrokerMessage{Payload: []byte(`{"name":"alice"}`)}
	err := mw(context.Background(), msg, next)

	require.Error(t, err)
	require.False(t, called, "next middleware must not be called for an invalid envelope")
}

func TestCloudEventsValidationRejectsMalformedJSON(t *testing.T) {
	mw := middlewares.CloudEventsValidation(validator.New(), func() any { return &cloudeventsPayload{} })

	called := false
	next := func(_ context.Context) error {
		called = true
		return nil
	}

	msg := &extensions.BrokerMessage{Payload: []byte(`{not-json`)}
	err := mw(context.Background(), msg, next)

	require.Error(t, err)
	require.False(t, called, "next middleware must not be called for malformed JSON")
}
