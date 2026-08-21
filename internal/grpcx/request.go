package grpcx

// Request is one gRPC call the console asks the process to make. It carries no
// build tag: the console posts this shape whether or not live calling was
// compiled in, and a build without it still has to decode the request to
// explain itself.
type Request struct {
	Target     string            `json:"target"`
	Symbol     string            `json:"symbol"` // package.Service/Method or package.Service.Method
	Data       string            `json:"data"`   // request body as JSON
	Headers    map[string]string `json:"headers,omitempty"`
	TLS        bool              `json:"tls,omitempty"`
	Insecure   bool              `json:"insecure,omitempty"`   // TLS but skip cert verify
	TimeoutSec int               `json:"timeoutSec,omitempty"` // 0 -> 15s
}
