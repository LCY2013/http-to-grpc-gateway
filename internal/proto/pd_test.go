package proto

import (
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"testing"
)

func TestNewProtoDescriptorBuilder(t *testing.T) {
	descriptorProto := NewProtoDescriptorBuilder("HelloRequest").
		WithField(
			"name",
			1,
			descriptor.FieldDescriptorProto_LABEL_OPTIONAL,
			descriptor.FieldDescriptorProto_TYPE_STRING).
		Builder()
	descriptorProto.ProtoReflect()
}
