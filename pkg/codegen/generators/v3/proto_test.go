package generatorv3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	asyncapi "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testHelloProto = `syntax = "proto3";

package hello.v1;

message HelloPayload {
  string greeting = 1;
}
`

// protoTestMessage builds a message with an inline proto schemaFormat payload.
func protoTestMessage(name, schemaFormat, schema string, ref *asyncapi.Message) *asyncapi.Message {
	msg := &asyncapi.Message{Name: name, Payload: &asyncapi.Schema{
		SchemaFormat: schemaFormat,
		SchemaInline: schema,
	}}
	if ref != nil {
		msg.ReferenceTo = ref
	}
	return msg
}

func TestProtoFilesDerivation(t *testing.T) {
	spec := asyncapi.Specification{
		Components: asyncapi.Components{
			Messages: map[string]*asyncapi.Message{
				"Hello": protoTestMessage("HelloMessage",
					"application/vnd.google.protobuf;version=3", testHelloProto, nil),
			},
		},
	}

	files, err := protoFiles(spec, "github.com/example/repo", "./asyncapi.gen.go")
	require.NoError(t, err)
	for _, file := range files {
		assert.Equal(t, filepath.Join("hello", "v1", "hello.proto"), file.RelPath)
		assert.Equal(t, strings.TrimSpace(testHelloProto), file.Content)
		assert.Equal(t, "hellov1.HelloPayload", file.PayloadGoType)
		assert.True(t, strings.HasSuffix(file.PayloadImport, "hello/v1"),
			"payload import should end with the proto dir, got %q", file.PayloadImport)
	}
}

func TestProtoFilesReferenceFollow(t *testing.T) {
	target := protoTestMessage("HelloMessage",
		"application/vnd.google.protobuf;version=3", testHelloProto, nil)
	ref := &asyncapi.Message{ReferenceTo: target}

	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{"Hello": target},
	}}

	// A channel message referencing the component resolves to the same
	// pointer: exactly one file for both entries.
	spec.Channels = map[string]*asyncapi.Channel{
		"hello": {Messages: map[string]*asyncapi.Message{
			"Hello": {Name: "HelloMessage", ReferenceTo: target},
			"Ref":   ref,
		}},
	}

	files, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.NoError(t, err)
	for _, file := range files {
		assert.Equal(t, "hellov1.HelloPayload", file.PayloadGoType)
	}
}

func TestProtoFilesSharedText(t *testing.T) {
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3", testHelloProto, nil),
			"Dolly": protoTestMessage("DollyMessage",
				"application/vnd.google.protobuf;version=3", testHelloProto, nil),
		},
	}}

	// DollyMessage's payload differs from Hello's expected name; identical
	// text but different expected message name must still validate per
	// message.
	_, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DollyMessage")
}

func TestProtoFilesConflictSamePackageDifferentText(t *testing.T) {
	other := `syntax = "proto3";

package hello.v1;

message OtherPayload {
  string renamed = 1;
}
`
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3", testHelloProto, nil),
			"Other": protoTestMessage("OtherMessage",
				"application/vnd.google.protobuf;version=3", other, nil),
		},
	}}

	_, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HelloMessage")
	assert.Contains(t, err.Error(), "OtherMessage")
	assert.Contains(t, err.Error(), "hello.v1")
}

func TestProtoFilesMissingPackage(t *testing.T) {
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3", "message Foo {}", nil),
		},
	}}

	_, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package")
}

func TestProtoFilesMissingPayloadMessage(t *testing.T) {
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3",
				"syntax = \"proto3\";\npackage hello.v1;\nmessage Foo {}", nil),
		},
	}}

	_, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HelloPayload")
	assert.Contains(t, err.Error(), "Foo")
}

func TestProtoFilesIgnoresJSONMessages(t *testing.T) {
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3", testHelloProto, nil),
			"Other": {Name: "OtherMessage", Payload: &asyncapi.Schema{}},
		},
	}}

	files, err := protoFiles(spec, "example.com/m", "asyncapi.gen.go")
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

func TestProtoFilesNestedDirectory(t *testing.T) {
	nested := `syntax = "proto3";

package ping.v1;

message PingPayload {
  message Detail { string pong = 1; }
  string ping = 1;
  Detail detail = 2;
}
`
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Ping": protoTestMessage("PingMessage",
				"application/vnd.google.protobuf;version=3", nested, nil),
		},
	}}

	files, err := protoFiles(spec, "example.com/repo", "asyncapi.gen.go")
	require.NoError(t, err)
	for _, file := range files {
		assert.Equal(t, filepath.Join("ping", "v1", "ping.proto"), file.RelPath)
		assert.Equal(t, "pingv1.PingPayload", file.PayloadGoType)
	}
}

func TestProtoFilesModulePathIncludesRelativeOutDir(t *testing.T) {
	spec := asyncapi.Specification{Components: asyncapi.Components{
		Messages: map[string]*asyncapi.Message{
			"Hello": protoTestMessage("HelloMessage",
				"application/vnd.google.protobuf;version=3", testHelloProto, nil),
		},
	}}

	files, err := protoFiles(spec, "example.com/repo",
		filepath.Join("some", "sub", "asyncapi.gen.go"))
	require.NoError(t, err)
	for _, file := range files {
		// The directory of the output file lands in the import path; the
		// go.mod discovery of moduleGoPrefix handles the real module path.
		assert.Contains(t, file.PayloadImport, "hello/v1")
	}
}

func TestModuleGoPrefixUsesNearestGoMod(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/themod\n\ngo 1.24\n"), 0o644))

	assert.Equal(t, "example.com/themod/nested/deep", moduleGoPrefix(sub, "fallback"))
	assert.Equal(t, "example.com/themod", moduleGoPrefix(dir, "fallback"))
}

func TestModuleGoPrefixFallbackWithoutGoMod(t *testing.T) {
	// Without any go.mod above the output directory, the prefix falls back
	// to the given module path, relative to the working directory.
	dir := t.TempDir()
	t.Chdir(dir) //nolint:usetesting // cwd control is the point of the test

	assert.Equal(t, "fallback", moduleGoPrefix(".", "fallback"))
	assert.Equal(t, "fallback/sub", moduleGoPrefix("sub", "fallback"))
}

func TestBufConfigsWriteOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeBufConfigs(dir, "example.com/m"))

	bufYAML, err := os.ReadFile(filepath.Join(dir, "buf.yaml"))
	require.NoError(t, err)
	bufGenYAML, err := os.ReadFile(filepath.Join(dir, "buf.gen.yaml"))
	require.NoError(t, err)

	// Second generation is byte-stable.
	require.NoError(t, writeBufConfigs(dir, "example.com/m"))

	bufYAML2, err := os.ReadFile(filepath.Join(dir, "buf.yaml"))
	require.NoError(t, err)
	bufGenYAML2, err := os.ReadFile(filepath.Join(dir, "buf.gen.yaml"))
	require.NoError(t, err)

	assert.Equal(t, string(bufYAML), string(bufYAML2))
	assert.Equal(t, string(bufGenYAML), string(bufGenYAML2))

	// User-owned files are never overwritten.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "buf.yaml"), []byte("custom: true\n"), 0o644))
	require.NoError(t, writeBufConfigs(dir, "example.com/m"))
	custom, err := os.ReadFile(filepath.Join(dir, "buf.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "custom: true\n", string(custom))
}

func TestBufConfigsLocalPlugin(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeBufConfigs(dir, "example.com/m"))

	bufGenYAML, err := os.ReadFile(filepath.Join(dir, "buf.gen.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(bufGenYAML), "local: protoc-gen-go")
	assert.NotContains(t, string(bufGenYAML), "remote:")
}

func TestBufNotInstalledError(t *testing.T) {
	cause := errors.New("not found")
	err := &BufNotInstalledError{cause: cause}

	assert.Contains(t, err.Error(), "buf is required for application/cloudevents+protobuf messages but was not found in PATH")
	assert.True(t, errors.Is(err, cause))
}
