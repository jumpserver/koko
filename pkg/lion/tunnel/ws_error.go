package tunnel

import (
	"strconv"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

type JMSGuacamoleError struct {
	code int
	msg  string
}

func (g JMSGuacamoleError) String() string {
	ins := g.Instruction()
	return ins.String()
}

func (g JMSGuacamoleError) Instruction() guacd.Instruction {
	return guacd.NewInstruction(
		guacd.InstructionServerError,
		g.msg,
		strconv.Itoa(g.code))
}

func NewJMSGuacamoleError(code int, msg string) JMSGuacamoleError {
	return JMSGuacamoleError{
		code: code,
		msg:  msg,
	}
}

const (
	InstructionJmsEvent = "jms_event"
)

func NewJmsEventInstruction(event string, jsonData string) guacd.Instruction {
	return guacd.NewInstruction(InstructionJmsEvent, event, jsonData)
}

// todo: 构造一种通用的错误框架，方便前后端处理异常

func NewJMSIdleTimeOutError(min int) JMSGuacamoleError {
	return NewJMSGuacamoleError(1003, strconv.Itoa(min))
}

func NewJMSMaxSessionTimeError(hour int) JMSGuacamoleError {
	return NewJMSGuacamoleError(1010, strconv.Itoa(hour))
}

var (
	ErrNoSession = NewJMSGuacamoleError(1000, "Not Found Session")

	ErrAuthUser = NewJMSGuacamoleError(1001, "Not auth user")

	ErrBadParams = NewJMSGuacamoleError(1002, "Not session params")

	//ErrIdleTimeOut = NewJMSGuacamoleError(1003, "Terminated by idle timeout")

	ErrPermissionExpired = NewJMSGuacamoleError(1004, "Terminated by permission expired")

	//ErrTerminatedByAdmin = NewJMSGuacamoleError(1005, "Terminated by Admin")

	ErrAPIFailed = NewJMSGuacamoleError(1006, "API failed")

	ErrGatewayFailed = NewJMSGuacamoleError(1007, "Gateway not available")

	ErrGuacamoleServer = NewJMSGuacamoleError(1008, "Connect guacamole server failed")

	ErrPermission = NewJMSGuacamoleError(256, "No permission")

	ErrDisconnect = NewJMSGuacamoleError(1009, "Disconnect by client")
)
