package proc

import "fmt"

type Identity struct {
	PID       int
	UID       uint32
	StartTime uint64
}

func (i Identity) Key() string {
	return fmt.Sprintf("%d:%d:%d", i.UID, i.PID, i.StartTime)
}
