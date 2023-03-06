package http

import (
	"errors"
	"http-to-grpc-gateway/internal/registry"
	"net/http"
	"strings"
)

type registerHttp struct {
	req *http.Request
}

func NewRegisterHttp(req *http.Request) registry.Register {
	return &registerHttp{
		req: req,
	}
}

func (hr registerHttp) Register() (*registry.Registry, error) {
	_, ok := hr.req.Header["Method"]
	if !ok {
		return nil, errors.New("method parameter not found")
	}
	headerMethod := hr.req.Header["Method"][0]

	methodSplit := strings.Split(headerMethod, "/")
	if len(methodSplit) != 2 {
		return nil, errors.New("method need form is foo.bar/method")
	}
	headerService := methodSplit[0]

	_, ok = hr.req.Header["Addr"]
	if !ok {
		return nil, errors.New("addr parameter not found")
	}
	headerAddr := hr.req.Header["Addr"][0]

	return &registry.Registry{
		Method:  headerMethod,
		Service: headerService,
		Addr:    headerAddr,
	}, nil
}
