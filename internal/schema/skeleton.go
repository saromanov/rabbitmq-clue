package schema

import (
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// FieldInfo describes a single message field for UI hints.
type FieldInfo struct {
	Path       string   `json:"path"`
	JSONName   string   `json:"name"`
	ProtoName  string   `json:"proto_name"`
	Kind       string   `json:"kind"`
	TypeName   string   `json:"type_name,omitempty"`
	Repeated   bool     `json:"repeated,omitempty"`
	Map        bool     `json:"map,omitempty"`
	Oneof      string   `json:"oneof,omitempty"`
	EnumValues []string `json:"enum_values,omitempty"`
}

// Skeleton returns a JSON skeleton (defaults for every field) of the message
// and a flat list of field descriptions for the UI.
func (c *Compiled) Skeleton(msgType string) (string, []FieldInfo, error) {
	md, err := c.descriptor(msgType)
	if err != nil {
		return "", nil, err
	}
	info := &skelInfo{}
	skel := skeletonMessage(md, 0, info, "")
	out, err := marshalIndent(skel)
	if err != nil {
		return "", nil, err
	}
	sort.Slice(info.fields, func(i, j int) bool { return info.fields[i].Path < info.fields[j].Path })
	return string(out), info.fields, nil
}

type skelInfo struct {
	fields []FieldInfo
}

func skeletonMessage(md protoreflect.MessageDescriptor, depth int, info *skelInfo, path string) any {
	obj := map[string]any{}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fieldPath := joinPath(path, fd.JSONName())
		fi := FieldInfo{
			Path:      fieldPath,
			JSONName:  fd.JSONName(),
			ProtoName: string(fd.Name()),
			Kind:      fieldKind(fd),
			Oneof:     oneofName(fd),
		}
		switch fd.Kind() {
		case protoreflect.EnumKind:
			fi.TypeName = string(fd.Enum().FullName())
			fi.EnumValues = enumValues(fd.Enum())
		case protoreflect.MessageKind:
			fi.TypeName = string(fd.Message().FullName())
		default:
			fi.TypeName = scalarTypeName(fd)
		}
		if fd.IsMap() {
			fi.Map = true
		}
		if fd.IsList() {
			fi.Repeated = true
		}
		info.fields = append(info.fields, fi)

		switch {
		case fd.IsMap():
			obj[fd.JSONName()] = map[string]any{}
		case fd.IsList():
			obj[fd.JSONName()] = []any{}
		case fd.Kind() == protoreflect.MessageKind:
			if special, ok := skeletonMessageSpecial(fd.Message()); ok {
				obj[fd.JSONName()] = special
			} else if depth >= maxSkeletonDepth {
				obj[fd.JSONName()] = nil
			} else {
				obj[fd.JSONName()] = skeletonMessage(fd.Message(), depth+1, info, fieldPath)
			}
		case fd.Kind() == protoreflect.EnumKind:
			obj[fd.JSONName()] = skeletonEnum(fd.Enum())
		default:
			obj[fd.JSONName()] = skeletonScalar(fd)
		}
	}
	return obj
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func fieldKind(fd protoreflect.FieldDescriptor) string {
	if fd.IsMap() {
		return "map"
	}
	if fd.IsList() {
		return "repeated"
	}
	switch fd.Kind() {
	case protoreflect.MessageKind:
		return "message"
	case protoreflect.EnumKind:
		return "enum"
	default:
		return "scalar"
	}
}

func oneofName(fd protoreflect.FieldDescriptor) string {
	if od := fd.ContainingOneof(); od != nil {
		return string(od.Name())
	}
	return ""
}

func enumValues(ed protoreflect.EnumDescriptor) []string {
	vals := ed.Values()
	out := make([]string, 0, vals.Len())
	for i := 0; i < vals.Len(); i++ {
		out = append(out, string(vals.Get(i).Name()))
	}
	return out
}

func skeletonEnum(ed protoreflect.EnumDescriptor) any {
	vals := ed.Values()
	if vals.Len() == 0 {
		return ""
	}
	return string(vals.Get(0).Name())
}

func skeletonMessageSpecial(md protoreflect.MessageDescriptor) (any, bool) {
	switch md.FullName() {
	case "google.protobuf.Timestamp":
		return "0001-01-01T00:00:00Z", true
	case "google.protobuf.Duration":
		return "0s", true
	case "google.protobuf.Struct":
		return map[string]any{}, true
	case "google.protobuf.Value":
		return nil, true
	case "google.protobuf.ListValue":
		return []any{}, true
	case "google.protobuf.Any":
		return map[string]any{"@type": ""}, true
	}
	return nil, false
}

func skeletonScalar(fd protoreflect.FieldDescriptor) any {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return false
	case protoreflect.StringKind:
		return ""
	case protoreflect.BytesKind:
		return ""
	default:
		return 0
	}
}

func scalarTypeName(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.Int64Kind:
		return "int64"
	case protoreflect.Uint64Kind:
		return "uint64"
	case protoreflect.Int32Kind:
		return "int32"
	case protoreflect.Fixed64Kind:
		return "fixed64"
	case protoreflect.Fixed32Kind:
		return "fixed32"
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.Uint32Kind:
		return "uint32"
	case protoreflect.EnumKind:
		return "enum"
	case protoreflect.Sfixed32Kind:
		return "sfixed32"
	case protoreflect.Sfixed64Kind:
		return "sfixed64"
	case protoreflect.Sint32Kind:
		return "sint32"
	case protoreflect.Sint64Kind:
		return "sint64"
	default:
		return fd.Kind().String()
	}
}
