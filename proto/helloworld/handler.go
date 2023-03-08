package helloworld

import "context"

type HelloworldService struct{}

func (s HelloworldService) SayHello(ctx context.Context, request *HelloRequest) (*HelloReply, error) {
	return &HelloReply{
		Message:    "hello, " + request.Name,
		Parameters: request.Parameters,
		Array:      request.Array,
		Body:       request.Body,
		Bytes:      request.Bytes,
	}, nil
}

func (s HelloworldService) mustEmbedUnimplementedGreeterServer() {

}
