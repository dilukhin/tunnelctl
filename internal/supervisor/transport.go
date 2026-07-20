package supervisor

import "context"

type controlServer interface {
	Requests() <-chan controlEnvelope
	Close() error
}

type controlEnvelope struct {
	Request Request
	Reply   func(Response)
}

func request(ctx context.Context, req Request) (Response, error) {
	return platformRequest(ctx, req)
}

func listenControl() (controlServer, error) {
	return platformListen()
}
