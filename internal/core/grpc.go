package core

type GrpcDoc struct {
	Package  string             `json:"package,omitempty"`
	Services []*GrpcService     `json:"services"`
	Messages map[string]*Schema `json:"messages"`
}

type GrpcService struct {
	Name     string        `json:"name"`
	FullName string        `json:"fullName"`
	Methods  []*GrpcMethod `json:"methods"`
}

type GrpcMethod struct {
	Name            string `json:"name"`
	InputType       string `json:"inputType"`
	OutputType      string `json:"outputType"`
	ClientStreaming bool   `json:"clientStreaming"`
	ServerStreaming bool   `json:"serverStreaming"`
}

func NewGrpcDoc() *GrpcDoc {
	return &GrpcDoc{
		Services: []*GrpcService{},
		Messages: map[string]*Schema{},
	}
}
