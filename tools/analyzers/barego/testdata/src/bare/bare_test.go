package bare

import "testing"

func TestConcurrent(t *testing.T) {
	done := make(chan struct{})
	go func() { close(done) }()
	<-done
}
