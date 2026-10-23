// Package templates holds the built-in format definitions used by the v3
// generator in addition to its template helpers.
package templates

import (
	"fmt"
	"strings"

	asyncapiv3 "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
	"github.com/albe2669/asyncapi-codegen/pkg/codegen/formats"
)

// CloudEventMediaType is the contentType of a message using CloudEvents in
// structured content mode. Optional parameters after ';' are ignored for
// matching.
const CloudEventMediaType = "application/cloudevents+json"

// CloudEventProtoMediaType is the contentType of a message carrying a
// CloudEvents v1 protobuf envelope, with the payload packed in the
// proto_data field of io.cloudevents.v1.CloudEvent.
const CloudEventProtoMediaType = "application/cloudevents+protobuf"

// protoSchemaFormat is the only payload schemaFormat supported with the
// protobuf CloudEvents format: an inline proto file for the payload.
const protoSchemaFormat = "application/vnd.google.protobuf;version=3"

func init() {
	formats.Register(cloudEventsFormat())
	formats.Register(cloudEventsProtoFormat())
}

// cloudEventsFormat returns the built-in CloudEvents format: the whole
// event, structured as one JSON object, travels as the broker payload.
func cloudEventsFormat() formats.Format {
	return formats.Format{
		MediaType: CloudEventMediaType,
		Imports:   []string{"github.com/cloudevents/sdk-go/v2/event"},
		Snippets: formats.Snippets{
			StructFields: `// Event is the embedded CloudEvents envelope, in structured content mode.
// Embedding gives direct access to its methods: msg.SetSource(...),
// msg.ID(), ...
event.Event
`,
			ConstructorBody: `
// Fill the required CloudEvents envelope attributes, so that the message
// can be published as is. Spec version: {{cloudEventSpecVersion .}}.
msg.Event = event.New("{{cloudEventSpecVersion .}}")
msg.SetID(uuid.New().String())
msg.SetSource("{{if .CloudEventSource}}{{.CloudEventSource}}{{else}}{{namify .Name}}{{end}}")
msg.SetType("{{namify .Name}}")
`,
			BrokerToMessage: `// Unmarshal the whole CloudEvents envelope from the payload
if err := json.Unmarshal(bMsg.Payload, &msg.Event); err != nil {
    return msg, err
}

// Fill the payload from the CloudEvents event data
if err := msg.DataAs(&msg.Payload); err != nil {
    return msg, err
}
`,
			MessageToBroker: `// Make sure the CloudEvents envelope is valid before sending
if verr := msg.Validate(); verr != nil {
    return extensions.BrokerMessage{}, verr
}

// Attach the payload as CloudEvents event data
if err := msg.SetData(event.ApplicationJSON, msg.Payload); err != nil {
    return extensions.BrokerMessage{}, err
}

// Marshal the whole CloudEvents envelope to the broker payload
payload, err := json.Marshal(msg.Event)
if err != nil {
    return extensions.BrokerMessage{}, err
}
`,
		},
		Validate: validateCloudEventMessage,
		Prepare:  prepareCloudEventMessage,
	}
}

// validateCloudEventMessage rejects CloudEvents spec versions that the
// generated code cannot emit.
func validateCloudEventMessage(msg *asyncapiv3.Message) error {
	version := CloudEventSpecVersion(msg)
	if version != "1.0" && version != "0.3" {
		return fmt.Errorf("message %q uses an unsupported CloudEvents spec version %q: supported versions are 0.3 and 1.0",
			msg.Follow().Name, version)
	}

	return nil
}

// prepareCloudEventMessage sets the default CloudEvents source from the
// specification title.
func prepareCloudEventMessage(spec *asyncapiv3.Specification, msg *asyncapiv3.Message) error {
	if spec != nil {
		msg.Follow().CloudEventSource = spec.Info.Title
	}

	return nil
}

// CloudEventSpecVersion returns the CloudEvents spec version to use, from
// the optional 'version' parameter of the contentType. It falls back to
// the latest spec version if the parameter is absent.
func CloudEventSpecVersion(msg *asyncapiv3.Message) string {
	if msg == nil {
		return "1.0"
	}

	parts := strings.Split(msg.Follow().ContentType, ";")

	for _, param := range parts[1:] {
		key, value, found := strings.Cut(strings.TrimSpace(param), "=")
		if !found || strings.TrimSpace(key) != "version" {
			continue
		}

		return strings.TrimSpace(value)
	}

	return "1.0"
}

// cloudEventsProtoFormat returns the built-in CloudEvents protobuf format:
// the broker payload is a serialized io.cloudevents.v1.CloudEvent envelope
// carrying the message payload in its proto_data field, typed by the Go code
// that buf generates from the message's inline proto schema.
func cloudEventsProtoFormat() formats.Format {
	return formats.Format{
		MediaType: CloudEventProtoMediaType,
		Imports: []string{
			"google.golang.org/protobuf/proto",
			"google.golang.org/protobuf/types/known/anypb",
		},
		Snippets: formats.Snippets{
			StructFields: `// CloudEvent is the embedded CloudEvents protobuf envelope. Embedding
// gives direct access to its fields for the io.cloudevents.v1 wire format.
pb.CloudEvent
`,
			ConstructorBody: `
// Fill the required CloudEvents envelope attributes, so that the message
// can be published as is.
msg.SpecVersion = "1.0"
msg.Id = uuid.New().String()
msg.Source = "{{if .CloudEventSource}}{{.CloudEventSource}}{{else}}{{namify .Name}}{{end}}"
msg.Type = "{{namify .Name}}"
`,
			BrokerToMessage: `// Unmarshal the CloudEvents protobuf envelope from the broker payload
if err := proto.Unmarshal(bMsg.Payload, &msg.CloudEvent); err != nil {
    return msg, err
}

// Unpack the payload from the CloudEvents proto_data field
anyPayload, ok := msg.CloudEvent.Data.(*pb.CloudEvent_ProtoData)
if !ok || anyPayload == nil {
    return msg, fmt.Errorf("message '{{namify .Name}}' has no proto_data in its CloudEvents envelope")
}
if err := anyPayload.ProtoData.UnmarshalTo(&msg.Payload); err != nil {
    return msg, err
}
`,
			MessageToBroker: `// Pack the payload in the CloudEvents proto_data field
anyPayload, err := anypb.New(&msg.Payload)
if err != nil {
    return extensions.BrokerMessage{}, err
}
msg.Data = &pb.CloudEvent_ProtoData{ProtoData: anyPayload}

// Marshal the CloudEvents protobuf envelope to the broker payload
payload, err := proto.Marshal(&msg.CloudEvent)
if err != nil {
    return extensions.BrokerMessage{}, err
}
`,
		},
		Validate: validateCloudEventProtoMessage,
		Prepare:  prepareCloudEventProtoMessage,
	}
}

// validateCloudEventProtoMessage rejects messages whose payload is not an
// inline proto schema, or whose CloudEvents spec version is unsupported.
func validateCloudEventProtoMessage(msg *asyncapiv3.Message) error {
	if version := CloudEventSpecVersion(msg); version != "1.0" {
		return fmt.Errorf("message %q: unsupported CloudEvents spec version %q for %s: expected 1.0",
			msg.Follow().Name, version, CloudEventProtoMediaType)
	}

	payload := msg.Follow().Payload
	if payload == nil {
		return fmt.Errorf("message %q: application/cloudevents+protobuf requires a payload with schemaFormat %q and its inline proto schema in 'schema'",
			msg.Follow().Name, protoSchemaFormat)
	}
	if payload.SchemaFormat != protoSchemaFormat {
		return fmt.Errorf("message %q: payload schemaFormat must be %q, got %q",
			msg.Follow().Name, protoSchemaFormat, payload.SchemaFormat)
	}
	if strings.TrimSpace(payload.SchemaInline) == "" {
		return fmt.Errorf("message %q: payload 'schema' must contain the inline proto definition for %s",
			msg.Follow().Name, CloudEventProtoMediaType)
	}

	return nil
}

// prepareCloudEventProtoMessage sets the default CloudEvents source from the
// specification title.
func prepareCloudEventProtoMessage(spec *asyncapiv3.Specification, msg *asyncapiv3.Message) error {
	if spec != nil {
		msg.Follow().CloudEventSource = spec.Info.Title
	}

	return nil
}
