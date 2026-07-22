package grpcx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

type Request struct {
	Target  string            `json:"target"`
	Symbol  string            `json:"symbol"` // package.Service/Method or package.Service.Method
	Data    string            `json:"data"`   // request body as JSON
	Headers map[string]string `json:"headers,omitempty"`
}

func Invoke(protoDir string, req Request) (string, error) {
	if req.Target == "" {
		return "", fmt.Errorf("target is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cc, err := grpc.NewClient(req.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", req.Target, err)
	}
	defer cc.Close()

	source, err := descriptorSource(ctx, protoDir, cc)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	handler := &grpcurl.DefaultEventHandler{
		Out:            &out,
		Formatter:      nil,
		VerbosityLevel: 0,
	}

	rf, formatter, err := grpcurl.RequestParserAndFormatter(
		grpcurl.FormatJSON, source, strings.NewReader(req.Data), grpcurl.FormatOptions{EmitJSONDefaultFields: true})
	if err != nil {
		return "", err
	}
	handler.Formatter = formatter

	var headers []string
	for k, v := range req.Headers {
		headers = append(headers, k+": "+v)
	}

	symbol := strings.Replace(req.Symbol, "/", ".", 1)
	if err := grpcurl.InvokeRPC(ctx, source, cc, symbol, headers, handler, rf.Next); err != nil {
		if out.Len() > 0 {
			return out.String(), nil
		}
		return "", err
	}
	if handler.Status != nil && handler.Status.Err() != nil {
		return out.String(), fmt.Errorf("%s: %s", handler.Status.Code(), handler.Status.Message())
	}
	return out.String(), nil
}

func descriptorSource(ctx context.Context, protoDir string, cc *grpc.ClientConn) (grpcurl.DescriptorSource, error) {
	files, err := protoFiles(protoDir)
	if err == nil && len(files) > 0 {
		src, derr := grpcurl.DescriptorSourceFromProtoFiles([]string{protoDir}, files...)
		if derr == nil {
			return src, nil
		}
		err = derr
	}
	// fall back to server reflection
	rc := grpcreflect.NewClientV1Alpha(ctx, reflectpb.NewServerReflectionClient(cc))
	return grpcurl.DescriptorSourceFromServer(ctx, rc), err
}

func protoFiles(dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("no proto dir")
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				rel = path
			}
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}
