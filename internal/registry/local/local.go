package local

import (
	"errors"
	"fmt"
	"github.com/LCY2013/http-to-grpc-gateway/internal/config"
	"github.com/LCY2013/http-to-grpc-gateway/internal/registry"
	"net/http"
	"strings"
)

type registerLocal struct {
	req *http.Request
}

func NewRegisterLocal(req *http.Request) registry.Register {
	return &registerLocal{
		req: req,
	}
}

func (hr registerLocal) Register() (*registry.Registry, error) {
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

	headerAddr, ok := config.LocalRegistry()[strings.ToLower(headerService)]
	if !ok || headerAddr == "" {
		return nil, fmt.Errorf("method name %q is not found", headerMethod)
	}

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
