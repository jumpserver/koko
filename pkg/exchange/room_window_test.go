package exchange

import (
	"testing"
	"time"
)

type windowTestStream struct{ messages chan *RoomMessage }

func (s *windowTestStream) Write(p []byte) (int, error) {
	s.messages <- &RoomMessage{Event: DataEvent, Body: p}
	return len(p), nil
}
func (*windowTestStream) Close() error                                 { return nil }
func (s *windowTestStream) HandleRoomEvent(_ string, msg *RoomMessage) { s.messages <- msg }

func TestMonitorReceivesLatestWindowBeforeRecentOutput(t *testing.T) {
	room := CreateRoom("test-window", make(chan *RoomMessage))
	go room.run()
	defer room.stop()
	room.Broadcast(&RoomMessage{Event: WindowsEvent, Body: []byte(`{"Width":80,"Height":24}`)})
	latest := &RoomMessage{Event: WindowsEvent, Body: []byte(`{"Width":120,"Height":30}`)}
	room.Broadcast(latest)
	room.Broadcast(&RoomMessage{Event: DataEvent, Body: []byte("dameng=>")})
	stream := &windowTestStream{messages: make(chan *RoomMessage, 8)}
	room.Subscribe(WrapperUserCon(stream))
	select {
	case message := <-stream.messages:
		if message.Event != WindowsEvent || string(message.Body) != string(latest.Body) {
			t.Fatalf("first monitor event = %s %s, want latest source window", message.Event, message.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor did not receive its source window")
	}
}
