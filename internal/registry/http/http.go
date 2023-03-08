package http

import (
	"errors"
	"fmt"
	"github.com/LCY2013/http-to-grpc-gateway/internal/registry"
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

	headerService := ""

	headerService, _ = parseSymbol(headerMethod)

	if headerMethod == "" || headerService == "" {
		return nil, fmt.Errorf("given method name %q is not in expected format: 'service/method' or 'service.method'", headerMethod)
	}

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

func parseSymbol(svcAndMethod string) (string, string) {
	pos := strings.LastIndex(svcAndMethod, "/")
	if pos < 0 {
		pos = strings.LastIndex(svcAndMethod, ".")
		if pos < 0 {
			return "", ""
		}
	}
	return svcAndMethod[:pos], svcAndMethod[pos+1:]
}
