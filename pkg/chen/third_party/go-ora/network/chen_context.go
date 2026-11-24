package network

import "context"

// StartContextWithAck permits a caller to finish a complete protocol operation
// before lending its connection to another operation. Unlike EndContext, the
// returned stop function joins the native-break sender, including cancel races.
// It must not be nested with another cancellable watcher on the same session.
func (session *Session) StartContextWithAck(ctx context.Context) func() {
	if ctx == nil || ctx.Done() == nil {
		return func() {}
	}
	stop := make(chan struct{})
	ack := make(chan struct{})
	go func() {
		defer close(ack)
		select {
		case <-stop:
			return
		case <-ctx.Done():
		}
		select {
		case <-stop:
			return
		default:
		}
		session.mu.Lock()
		connected := session.Connected
		tracer := session.tracer
		session.mu.Unlock()
		if connected {
			if err := session.BreakConnection(); err != nil {
				tracer.Print("Connection Break Error: ", err)
			}
		}
	}()
	return func() { close(stop); <-ack }
}
