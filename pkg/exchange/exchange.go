package exchange

import "github.com/jumpserver/koko/pkg/model"

type Exchanger interface {
	CreateRoom(receiveChan chan<- model.RoomMessage, roomId string) Room

	DestroyRoom(Room)

	JoinRoom(receiveChan chan<- model.RoomMessage, roomId string) (Room, error)

	LeaveRoom(sub Room, roomId string)

	Close()
}

type Room interface {
	Publish(msg model.RoomMessage)
}
