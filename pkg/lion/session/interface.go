package session

type ParseEngine interface {
	ParseStream(userInChan chan *Message)

	Close()

	CommandRecordChan() chan *ExecutedCommand
}
