// Package services exposes Wails-bound service types for the GUI frontend.
package services

// GreetService is a demo Wails service.
type GreetService struct{}

// Greet returns a greeting for the given name.
func (g *GreetService) Greet(name string) string {
	return "Hello " + name + "!"
}
