//go:build !linux

package proc

func FindAgent(_ int, _ uint32, _ []string) (Identity, bool) { return Identity{}, false }
func Alive(_ Identity) bool                                  { return false }
