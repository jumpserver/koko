package proxybak

type Slot interface {
	Chan() chan<- []byte
	Send([]byte)
}
