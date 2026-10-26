// Package schema compiles protobuf source files in memory and lets callers
// encode/decode RabbitMQ payloads against the compiled schemas without
// protoc or generated code (dynamic messages).
package schema

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/reporter"
)

const (
	MaxFiles         = 25
	MaxFileSize      = 512 << 10
	maxSkeletonDepth = 8
)

// Compiled holds fully linked file descriptors of a user-supplied schema.
// A Compiled value is immutable and safe for concurrent use.
type Compiled struct {
	msgs  map[string]protoreflect.MessageDescriptor
	enums []protoreflect.EnumDescriptor
	types *protoregistry.Types
	root  []string
}

// Compile builds the schema from in-memory proto source files.
// keys are file names (may include subdirectories), values are proto sources.
// Standard imports (google/protobuf/*.proto) are always available.
func Compile(files map[string]string) (*Compiled, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("schema has no files")
	}
	if len(files) > MaxFiles {
		return nil, fmt.Errorf("too many files: %d > %d", len(files), MaxFiles)
	}
	for name, src := range files {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("empty file name")
		}
		if strings.Contains(name, "..") {
			return nil, fmt.Errorf("invalid file name %q", name)
		}
		if len(src) > MaxFileSize {
			return nil, fmt.Errorf("file %s too large: %d > %d bytes", name, len(src), MaxFileSize)
		}
	}

	var srcErr error
	rep := reporter.NewReporter(func(e reporter.ErrorWithPos) error {
		srcErr = e
		return e
	}, nil)

	comp := &protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(mapResolver(files)),
		SourceInfoMode: protocompile.SourceInfoNone,
		Reporter:       rep,
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	fdset, err := comp.Compile(context.Background(), names...)
	if err != nil {
		if srcErr != nil {
			return nil, fmt.Errorf("compile schema: %w", srcErr)
		}
		return nil, fmt.Errorf("compile schema: %w", err)
	}

	c := &Compiled{msgs: map[string]protoreflect.MessageDescriptor{}, root: names}
	var enums []protoreflect.EnumDescriptor
	for _, fd := range fdset {
		c.collectMessages(fd.Messages())
		for i := 0; i < fd.Enums().Len(); i++ {
			enums = append(enums, fd.Enums().Get(i))
		}
	}

	types := new(protoregistry.Types)
	for name, md := range c.msgs {
		if err := types.RegisterMessage(dynamicMessageType(md)); err != nil {
			return nil, fmt.Errorf("register type %s: %w", name, err)
		}
	}
	for _, ed := range enums {
		if err := types.RegisterEnum(dynamicEnumType(ed)); err != nil {
			return nil, fmt.Errorf("register enum %s: %w", string(ed.FullName()), err)
		}
	}
	c.types = types
	return c, nil
}

type mapResolver map[string]string

func (m mapResolver) FindFileByPath(name string) (protocompile.SearchResult, error) {
	src, ok := m[name]
	if !ok {
		return protocompile.SearchResult{}, protoregistry.NotFound
	}
	return protocompile.SearchResult{Source: strings.NewReader(src)}, nil
}

func (c *Compiled) collectMessages(list protoreflect.MessageDescriptors) {
	for i := 0; i < list.Len(); i++ {
		md := list.Get(i)
		if md.IsMapEntry() {
			continue
		}
		c.msgs[string(md.FullName())] = md
		for j := 0; j < md.Enums().Len(); j++ {
			c.enums = append(c.enums, md.Enums().Get(j))
		}
		c.collectMessages(md.Messages())
	}
}

// Types returns the sorted full names of all messages in the schema.
func (c *Compiled) Types() []string {
	out := make([]string, 0, len(c.msgs))
	for name := range c.msgs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// HasType reports whether the schema contains a message with the given full name.
func (c *Compiled) HasType(name string) bool {
	_, ok := c.msgs[name]
	return ok
}

func (c *Compiled) descriptor(msgType string) (protoreflect.MessageDescriptor, error) {
	md, ok := c.msgs[msgType]
	if !ok {
		return nil, fmt.Errorf("unknown message type %q in schema", msgType)
	}
	return md, nil
}

func (c *Compiled) newMessage(msgType string) (proto.Message, error) {
	md, err := c.descriptor(msgType)
	if err != nil {
		return nil, err
	}
	return dynamicMessage(md), nil
}

// jsonResolver resolves types from this schema first, falling back to the
// global registry (well-known types).
func (c *Compiled) jsonResolver() protojsonResolver {
	return protojsonResolver{local: c.types}
}

type protojsonResolver struct {
	local *protoregistry.Types
}

func (r protojsonResolver) FindMessageByName(name protoreflect.FullName) (protoreflect.MessageType, error) {
	if mt, err := r.local.FindMessageByName(name); err == nil {
		return mt, nil
	}
	return protoregistry.GlobalTypes.FindMessageByName(name)
}

func (r protojsonResolver) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	if mt, err := r.local.FindMessageByURL(url); err == nil {
		return mt, nil
	}
	return protoregistry.GlobalTypes.FindMessageByURL(url)
}

func (r protojsonResolver) FindEnumByName(name protoreflect.FullName) (protoreflect.EnumType, error) {
	if et, err := r.local.FindEnumByName(name); err == nil {
		return et, nil
	}
	return protoregistry.GlobalTypes.FindEnumByName(name)
}

func (r protojsonResolver) FindExtensionByName(field protoreflect.FullName) (protoreflect.ExtensionType, error) {
	if xt, err := r.local.FindExtensionByName(field); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByName(field)
}

func (r protojsonResolver) FindExtensionByNumber(message protoreflect.FullName, field protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	if xt, err := r.local.FindExtensionByNumber(message, field); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByNumber(message, field)
}
