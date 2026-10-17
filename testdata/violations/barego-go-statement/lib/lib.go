// Package lib is a violation fixture.
package lib

func Fire() {
	go func() {}()
}
