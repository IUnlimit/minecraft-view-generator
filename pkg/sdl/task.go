package sdl

// Do runs f on the main thread.
func Do(f func()) {
	done := make(chan bool, 1)
	mainFunc <- func() {
		f()
		done <- true
	}
	<-done
}
