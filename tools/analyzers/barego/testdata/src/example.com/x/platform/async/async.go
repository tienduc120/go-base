package async

// Go is the one supervised way to start a goroutine.
func Go(f func()) {
	go f()
}
