package schema

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func dynamicMessage(md protoreflect.MessageDescriptor) *dynamicpb.Message {
	return dynamicpb.NewMessage(md)
}

func dynamicMessageType(md protoreflect.MessageDescriptor) protoreflect.MessageType {
	return dynamicpb.NewMessageType(md)
}

func dynamicEnumType(ed protoreflect.EnumDescriptor) protoreflect.EnumType {
	return dynamicpb.NewEnumType(ed)
}
