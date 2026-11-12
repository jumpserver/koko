package guacd

import "fmt"

// ProtocolError preserves the error instruction returned by guacd.
type ProtocolError struct {
	Instruction Instruction
}

func (e *ProtocolError) Error() string {
	if len(e.Instruction.Args) >= 2 {
		return fmt.Sprintf("guacd error %s: %s", e.Instruction.Args[1], e.Instruction.Args[0])
	}
	return fmt.Sprintf("guacd error: %s", e.Instruction.String())
}
