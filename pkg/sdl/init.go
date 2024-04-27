package sdl

import "runtime"

// Init Arrange that main.main runs on main thread.
func Init() {
	runtime.LockOSThread()
	main()
}

// main runs the main SDL service loop.
// The binary's main.main must call sdl.Main() to run this loop.
// Main does not return. If the binary needs to do other work, it
// must do it in separate goroutines.
func main() {
	for f := range mainFunc {
		f()
	}
}

// queue of work to run in main thread.
var mainFunc = make(chan func())

// Do runs f on the main thread.
func Do(f func()) {
	done := make(chan bool, 1)
	mainFunc <- func() {
		f()
		done <- true
	}
	<-done
}
