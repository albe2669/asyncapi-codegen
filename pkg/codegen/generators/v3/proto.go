package generatorv3

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	asyncapi "github.com/albe2669/asyncapi-codegen/pkg/asyncapi/v3"
)

// protoFile is a proto file to be written into the output directory, together
// with the payload Go type name and import path it provides.
type protoFile struct {
	// RelPath is the file path relative to the output directory.
	RelPath string
	// Content is the verbatim inline proto text from the AsyncAPI message.
	Content string
	// PayloadGoType is the package-qualified Go struct name of the payload,
	// e.g. "hellov1.HelloPayload".
	PayloadGoType string
	// PayloadImport is the Go import path of the compiled payload package.
	PayloadImport string
}

var protoPackageRegexp = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z0-9_.]+)\s*;`)

var protoMessageRegexp = regexp.MustCompile(`(?m)^\s*message\s+([A-Za-z0-9_]+)\s*\{`)

// protoPackageName extracts the proto package declaration from inline proto
// source, e.g. "hello.v1" for "package hello.v1;".
func protoPackageName(protoSrc string) (string, error) {
	match := protoPackageRegexp.FindStringSubmatch(protoSrc)
	if match == nil {
		return "", fmt.Errorf("inline proto schema has no 'package' declaration")
	}
	return match[1], nil
}

// protoMessageNames lists the top-level message names declared by the inline
// proto source.
func protoMessageNames(protoSrc string) []string {
	matches := protoMessageRegexp.FindAllStringSubmatch(protoSrc, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

// protoDirPath converts a proto package name to its file path, e.g.
// "hello.v1" becomes "hello/v1".
func protoDirPath(protoPkg string) string {
	return strings.ReplaceAll(protoPkg, ".", "/")
}

// protoFileName derives the proto file name from the package declaration:
// "hello.v1" gives "hello.proto", stored under "hello/v1/".
func protoFileName(protoPkg string) string {
	return strings.SplitN(protoPkg, ".", 2)[0] + ".proto"
}

// protoGoPackageName converts a proto package name to its Go package name,
// e.g. "hello.v1" becomes "hellov1".
func protoGoPackageName(protoPkg string) string {
	return strings.ReplaceAll(protoPkg, ".", "")
}

// protoFiles derives every proto file to write from the messages using the
// protobuf CloudEvents format, keyed by the message they belong to.
//
// Two messages referencing the same proto text are allowed: both get the
// same compiled types. Two messages declaring different proto text for the
// same proto package are a wire-contract conflict and fail generation.
func protoFiles(spec asyncapi.Specification, modulePath, outputPath string) (map[*asyncapi.Message]protoFile, error) {
	// Collect each distinct message (component messages and revisits resolve
	// to the same pointers via Follow).
	msgs := make(map[*asyncapi.Message]struct{})
	for _, msg := range spec.Components.Messages {
		msgs[msg.Follow()] = struct{}{}
	}
	for _, ch := range spec.Channels {
		for _, msg := range ch.Follow().Messages {
			msgs[msg.Follow()] = struct{}{}
		}
	}

	// One proto file per distinct proto text; payloads keyed by message.
	byText := make(map[string]protoFile)
	pkgText := make(map[string]string)
	pkgOwner := make(map[string]string)
	files := make(map[*asyncapi.Message]protoFile)

	for msg := range msgs {
		payload := msg.Payload
		if payload == nil || payload.SchemaFormat != "application/vnd.google.protobuf;version=3" {
			continue
		}

		text := strings.TrimSpace(payload.SchemaInline)

		protoPkg, err := protoPackageName(text)
		if err != nil {
			return nil, fmt.Errorf("message %q: %w", msg.Name, err)
		}

		// The payload message keeps the AsyncAPI message name, dropping its
		// 'Message' suffix: Hello (Message) expects HelloPayload.
		wantMsg := strings.TrimSuffix(msg.Name, "Message") + "Payload"
		if !containsString(protoMessageNames(text), wantMsg) {
			return nil, fmt.Errorf(
				"message %q: inline proto schema declares no %q message; found %v",
				msg.Name, wantMsg, protoMessageNames(text))
		}

		// Differing proto text for the same proto package is a wire-contract
		// conflict between the two messages.
		if prev, found := pkgText[protoPkg]; found && prev != text {
			return nil, fmt.Errorf(
				"messages %q and %q declare different inline proto text for the same proto package %q",
				pkgOwner[protoPkg], msg.Name, protoPkg)
		}
		if _, found := pkgText[protoPkg]; !found {
			pkgText[protoPkg] = text
			pkgOwner[protoPkg] = msg.Name
		}

		if existing, found := byText[text]; found {
			files[msg] = protoFile{
				RelPath:       existing.RelPath,
				Content:       text,
				PayloadGoType: protoGoPackageName(protoPkg) + "." + wantMsg,
				PayloadImport: existing.PayloadImport,
			}
			continue
		}

		file := protoFile{
			RelPath:       filepath.Join(protoDirPath(protoPkg), protoFileName(protoPkg)),
			Content:       text,
			PayloadGoType: protoGoPackageName(protoPkg) + "." + wantMsg,
			PayloadImport: moduleGoPrefix(filepath.Dir(outputPath), modulePath) + "/" + protoDirPath(protoPkg),
		}
		byText[text] = file
		files[msg] = file
	}

	return files, nil
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// runBufForProtos runs the full buf pipeline for messages using the protobuf
// CloudEvents format: it snapshots the previous protos, writes the fresh
// ones, runs buf lint and buf breaking, then buf generate. It does nothing
// when no message uses the format.
func (g Generator) runBufForProtos() error {
	files, err := protoFiles(g.Specification, g.ModulePath, g.Options.OutputPath)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	outDir := filepath.Dir(g.Options.OutputPath)
	if err := g.checkBufAvailable(); err != nil {
		return err
	}

	baseline, err := snapshotProtos(outDir)
	if err != nil {
		return err
	}

	if err := writeCloudEventsProto(outDir); err != nil {
		return err
	}

	if err := writeProtoFiles(outDir, files); err != nil {
		return err
	}

	if err := writeBufConfigs(outDir, g.ModulePath); err != nil {
		return err
	}

	if err := runBuf(outDir, "lint"); err != nil {
		// A lint failure is a rejected contract: keep the last generated
		// protos, and surface the buf error.
		if restoreErr := restoreProtos(outDir, baseline); restoreErr != nil {
			return fmt.Errorf("%w (also failed to restore previous protos: %v)", err, restoreErr)
		}
		return err
	}

	if err := g.runBufBreaking(outDir, baseline); err != nil {
		// A rejected contract must not persist in the output directory:
		// put back the last generated protos, and surface the buf error.
		if restoreErr := restoreProtos(outDir, baseline); restoreErr != nil {
			return fmt.Errorf("%w (also failed to restore previous protos: %v)", err, restoreErr)
		}
		return err
	}

	if err := runBuf(outDir, "generate"); err != nil {
		// The generated .pb.go files may be half-written or stale; restore
		// the proto sources so the next run starts from the known state,
		// and surface the buf error.
		if restoreErr := restoreProtos(outDir, baseline); restoreErr != nil {
			return fmt.Errorf("%w (also failed to restore previous protos: %v)", err, restoreErr)
		}
		return err
	}

	g.applyProtoPayloadTypes(files)

	return nil
}

// checkBufAvailable fails early with a helpful message when buf is missing.
func (g Generator) checkBufAvailable() error {
	if _, err := execLookPathBuf(); err != nil {
		return &BufNotInstalledError{cause: err}
	}
	return nil
}

// runBufBreaking checks the fresh protos of outDir against the baseline, or
// against the explicit --breaking-against target when one is configured.
func (g Generator) runBufBreaking(outDir, baseline string) error {
	if g.Options.BreakingAgainst != "" {
		return runBuf(outDir, "breaking", outDir, "--against", g.Options.BreakingAgainst)
	}
	if baseline == "" {
		// First generation: nothing to break against.
		return nil
	}
	return runBuf(outDir, "breaking", outDir, "--against", baseline)
}

// restoreProtos puts the baseline snapshot back over the generated protos of
// outDir, so a rejected contract leaves no partial state behind. A nil or
// empty baseline (first generation) deletes the freshly written protos.
func restoreProtos(outDir, baseline string) error {
	// List what the run just wrote or regenerated.
	var current []string
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			current = append(current, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if baseline == "" {
		// First generation: no baseline to restore, remove the fresh protos.
		for _, p := range current {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
		return nil
	}

	// Replace the proto tree with the baseline snapshot.
	for _, p := range current {
		if err := os.Remove(p); err != nil {
			return err
		}
	}

	return filepath.WalkDir(baseline, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseline, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

// cloudEventsProtoSource is the official CloudEvents v1 protobuf format
// definition, kept verbatim from the CloudEvents specification so the
// generated code and the wire format stay deterministic.
const cloudEventsProtoSource = `syntax = "proto3";

package io.cloudevents.v1;

import "google/protobuf/any.proto";
import "google/protobuf/timestamp.proto";

message CloudEvent {
  string id = 1;
  string source = 2;
  string spec_version = 3;
  string type = 4;
  map<string, CloudEventAttributeValue> attributes = 5;
  oneof data {
    bytes binary_data = 6;
    string text_data = 7;
    google.protobuf.Any proto_data = 8;
  }

  message CloudEventAttributeValue {
    oneof attr {
      bool ce_boolean = 1;
      int32 ce_integer = 2;
      string ce_string = 3;
      bytes ce_bytes = 4;
      string ce_uri = 5;
      string ce_uri_ref = 6;
      google.protobuf.Timestamp ce_timestamp = 7;
    }
  }
}
`

// writeCloudEventsProto vendors the CloudEvents envelope proto into outDir,
// only when absent: the file is user-owned after the first generation.
func writeCloudEventsProto(outDir string) error {
	dst := filepath.Join(outDir, "io", "cloudevents", "v1", "cloudevents.proto")
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(cloudEventsProtoSource), 0o644)
	} else if err != nil {
		return err
	}

	return nil
}

// execLookPathBuf locates the buf binary for the early availability check.
func execLookPathBuf() (string, error) {
	return exec.LookPath("buf")
}

// snapshotProtos copies every existing proto file of outDir into a temporary
// directory, so the fresh protos can be compared against the last generated
// output. It returns "" when outDir contains no proto files (first
// generation).
func snapshotProtos(outDir string) (string, error) {
	var protoFiles []string
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to snapshot previous protos: %w", err)
	}
	if len(protoFiles) == 0 {
		return "", nil
	}

	snapDir, err := os.MkdirTemp("", "asyncapi-proto-baseline-*")
	if err != nil {
		return "", fmt.Errorf("failed to create baseline snapshot dir: %w", err)
	}

	for _, src := range protoFiles {
		rel, err := filepath.Rel(outDir, src)
		if err != nil {
			return "", err
		}
		dst := filepath.Join(snapDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", err
		}
	}

	return snapDir, nil
}

// writeProtoFiles writes the derived proto files verbatim into outDir.
// Existing proto files at the same paths are overwritten: the specification
// is the source of truth for payloads.
func writeProtoFiles(outDir string, files map[*asyncapi.Message]protoFile) error {
	seen := make(map[string]protoFile)
	for _, file := range files {
		seen[file.RelPath] = file
	}

	paths := make([]string, 0, len(seen))
	for relPath := range seen {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		dst := filepath.Join(outDir, relPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(seen[relPath].Content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// writeBufConfigs writes buf.yaml and buf.gen.yaml into outDir, only when
// absent: after the first generation the files are user-owned and never
// overwritten.
func writeBufConfigs(outDir, modulePath string) error {
	if _, err := os.Stat(filepath.Join(outDir, "buf.yaml")); os.IsNotExist(err) {
		bufYAML := "version: v2\nmodules:\n  - path: .\nlint:\n  use:\n    - STANDARD\nbreaking:\n  use:\n    - FILE\n"
		if err := os.WriteFile(filepath.Join(outDir, "buf.yaml"), []byte(bufYAML), 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(outDir, "buf.gen.yaml")); os.IsNotExist(err) {
		bufGenYAML := "version: v2\nmanaged:\n  enabled: true\n  override:\n    - file_option: go_package_prefix\n      value: " + moduleGoPrefix(outDir, modulePath) + "\nplugins:\n  - local: protoc-gen-go\n    out: .\n    opt: paths=source_relative\n"
		if err := os.WriteFile(filepath.Join(outDir, "buf.gen.yaml"), []byte(bufGenYAML), 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

// moduleGoPrefix returns the Go import prefix of the generated protos,
// derived from the go module that owns the output directory: the nearest
// go.mod found above outDir gives the module path, and outDir is joined as
// package directories below it.
func moduleGoPrefix(outDir, modulePath string) string {
	absDir, err := filepath.Abs(outDir)
	if err != nil {
		absDir = outDir
	}

	goModDir, goModModule := findGoMod(absDir)
	if goModModule == "" {
		// No go module: fall back to the generator's module path, with the
		// output directory relative to the working directory.
		wd, err := os.Getwd()
		if err != nil {
			return modulePath
		}
		rel, err := filepath.Rel(wd, absDir)
		if err != nil {
			return modulePath
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "." {
			return modulePath
		}
		return modulePath + "/" + relSlash
	}

	rel, err := filepath.Rel(goModDir, absDir)
	if err != nil {
		return modulePath
	}
	relSlash := filepath.ToSlash(rel)
	if relSlash == "." {
		return goModModule
	}
	return goModModule + "/" + relSlash
}

// findGoMod returns the directory and module path of the nearest go.mod file
// at or above dir. Empty module path means no go.mod was found.
func findGoMod(dir string) (dirPath, module string) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			if mod := parseGoModModule(data); mod != "" {
				return dir, mod
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// parseGoModModule extracts the module path from go.mod content.
func parseGoModModule(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, found := strings.CutPrefix(line, "module "); found {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// applyProtoPayloadTypes sets the x-go-type and x-go-type-import extensions
// on each proto-format message payload, so the message struct field resolves
// to the buf-compiled payload type.
func (g Generator) applyProtoPayloadTypes(files map[*asyncapi.Message]protoFile) {
	for msg, file := range files {
		payload := msg.Payload
		if payload == nil {
			continue
		}
		payload.ExtGoType = file.PayloadGoType
		payload.ExtGoTypeImport = &asyncapi.GoTypeImportExtension{
			Path: asyncapi.GoTypeImportPath(file.PayloadImport),
		}
	}
}
