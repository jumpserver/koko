package exchange

import (
	"fmt"
	"sync"

	"github.com/jumpserver/koko/pkg/model"
)

func NewLocalExchange() (*LocalExchange, error) {
	return &LocalExchange{
		createdRooms: make(map[string]*localRoom),
		joinedRooms:  make(map[string]map[*localRoom]chan<- model.RoomMessage),
	}, nil

}

type LocalExchange struct {
	createdRooms map[string]*localRoom
	joinedRooms  map[string]map[*localRoom]chan<- model.RoomMessage
	mu           sync.Mutex
}

func (exc *LocalExchange) JoinRoom(receiveChan chan<- model.RoomMessage, roomId string) (Room, error) {
	exc.mu.Lock()
	defer exc.mu.Unlock()
	if createdRoom, ok := exc.createdRooms[roomId]; ok {
		r := &localRoom{
			roomID:    roomId,
			writeChan: createdRoom.readChan,
			readChan:  receiveChan,
		}
		if joinRoomsMap, ok := exc.joinedRooms[roomId]; ok {
			joinRoomsMap[r] = receiveChan
		} else {
			exc.joinedRooms[roomId] = map[*localRoom]chan<- model.RoomMessage{
				r: receiveChan,
			}
		}
		return r, nil
	}

	return nil, fmt.Errorf("room %s not found", roomId)
}

func (exc *LocalExchange) LeaveRoom(exRoom Room, roomId string) {
	sub, ok := exRoom.(*localRoom)
	if !ok {
		return
	}
	exc.mu.Lock()
	defer exc.mu.Unlock()
	if joinRoomsMap, ok := exc.joinedRooms[roomId]; ok {
		delete(joinRoomsMap, sub)
	}
	close(sub.readChan)
}

func (exc *LocalExchange) CreateRoom(receiveChan chan<- model.RoomMessage, roomId string) Room {
	exc.mu.Lock()
	defer exc.mu.Unlock()
	readChan := make(chan model.RoomMessage)
	r := &localRoom{
		roomID:    roomId,
		writeChan: readChan,
		readChan:  receiveChan,
	}
	exc.createdRooms[roomId] = r
	go func() {
		for {
			roomMgs, ok := <-readChan
			if !ok {
				return
			}
			exc.mu.Lock()
			joinedRooms := make([]chan<- model.RoomMessage, 0, len(exc.joinedRooms[roomId]))
			for _, roomChan := range exc.joinedRooms[roomId] {
				joinedRooms = append(joinedRooms, roomChan)
			}
			exc.mu.Unlock()
			for i := range joinedRooms {
				select {
				case joinedRooms[i] <- roomMgs:
				default:

				}
			}
		}
	}()
	return r
}

func (exc *LocalExchange) DestroyRoom(exRoom Room) {
	sub, ok := exRoom.(*localRoom)
	if !ok {
		return
	}
	exc.mu.Lock()
	defer exc.mu.Unlock()
	delete(exc.createdRooms, sub.roomID)
	close(sub.readChan)
}

func (exc *LocalExchange) Close() {
	exc.mu.Lock()
	defer exc.mu.Unlock()
	for roomID, createdRoom := range exc.createdRooms {
		if joinRoomMap, ok := exc.joinedRooms[roomID]; ok {
			delete(exc.joinedRooms, roomID)
			for joinedRoom := range joinRoomMap {
				close(joinedRoom.readChan)
			}
		}
		close(createdRoom.readChan)
	}

}

type localRoom struct {
	roomID    string
	writeChan chan<- model.RoomMessage
	readChan  chan<- model.RoomMessage
}

func (r *localRoom) Publish(msg model.RoomMessage) {
	r.writeChan <- msg
}
