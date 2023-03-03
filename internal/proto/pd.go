package proto

import (
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"google.golang.org/protobuf/types/descriptorpb"
)

type BuilderProtoDescriptor struct {
	Name   string
	Fields []*descriptor.FieldDescriptorProto
}

func NewProtoDescriptorBuilder(dpName string) *BuilderProtoDescriptor {
	return &BuilderProtoDescriptor{
		Name: dpName,
	}
}

func (bpd *BuilderProtoDescriptor) WithField(
	fieldName string,
	seat int32,
	fdptl descriptorpb.FieldDescriptorProto_Label,
	fdpt descriptorpb.FieldDescriptorProto_Type) *BuilderProtoDescriptor {
	bpd.Fields = append(bpd.Fields, &descriptor.FieldDescriptorProto{
		Name:     proto.String(fieldName),
		JsonName: proto.String(fieldName),
		Number:   proto.Int32(seat),
		Label:    fdptl.Enum(),
		Type:     fdpt.Enum(),
	})
	return bpd
}

func (bpd *BuilderProtoDescriptor) Builder() *descriptor.DescriptorProto {
	return &descriptor.DescriptorProto{
		Name:           proto.String(bpd.Name),
		Field:          bpd.Fields,
		Extension:      nil,
		NestedType:     nil,
		EnumType:       nil,
		ExtensionRange: nil,
		OneofDecl:      nil,
		Options:        nil,
		ReservedRange:  nil,
		ReservedName:   nil,
	}
	/*return &descriptor.DescriptorProto{
		Name: proto.String("Address"),
		Field: []*descriptor.FieldDescriptorProto{
			&descriptor.FieldDescriptorProto{
				Name:     proto.String("street"),
				JsonName: proto.String("street"),
				Number:   proto.Int(1),
				Label:    descriptor.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptor.FieldDescriptorProto_TYPE_STRING.Enum(),
			},

			&descriptor.FieldDescriptorProto{
				Name:     proto.String("state"),
				JsonName: proto.String("state"),
				Number:   proto.Int(2),
				Label:    descriptor.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptor.FieldDescriptorProto_TYPE_STRING.Enum(),
			},

			&descriptor.FieldDescriptorProto{
				Name:     proto.String("country"),
				JsonName: proto.String("country"),
				Number:   proto.Int(2),
				Label:    descriptor.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptor.FieldDescriptorProto_TYPE_STRING.Enum(),
			},
		},
	}, nil*/
}
