package guacd

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSocketTimeOut = time.Second * 15

	Version = "VERSION_1_3_0"
)

func NewTunnel(address string, config Configuration, info ClientInformation) (tunnel *Tunnel, err error) {
	var conn net.Conn
	conn, err = net.DialTimeout("tcp", address, defaultSocketTimeOut)
	if err != nil {
		return nil, err
	}
	defer func() {
		// 如果err 则直接关闭 连接
		if err != nil {
			_ = conn.Close()
			log.Printf("关闭连接防止conn未关闭,%s\n", err.Error())
		}
	}()
	tunnel = &Tunnel{}
	tunnel.conn = conn
	tunnel.rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	tunnel.decoder = NewInstructionDecoder(tunnel.rw.Reader)
	tunnel.Config = config

	selectArg := config.ConnectionID
	if selectArg == "" {
		selectArg = config.Protocol
	}

	// Send requested protocol or connection ID
	if err = tunnel.WriteInstructionAndFlush(NewInstruction("select", selectArg)); err != nil {
		return nil, err
	}
	var connectArgs Instruction

	// Wait for server args
	connectArgs, err = tunnel.expect("args")
	if err != nil {
		return nil, err
	}
	// Build args list off provided names and config
	connectArgsValues := make([]string, len(connectArgs.Args))
	for i := range connectArgs.Args {
		argName := connectArgs.Args[i]
		if strings.HasPrefix(argName, "VERSION") {
			connectArgsValues[i] = Version
			continue
		}
		connectArgsValues[i] = config.GetParameter(argName)
	}

	// send size
	width := info.OptimalScreenWidth
	height := info.OptimalScreenHeight
	dpi := info.OptimalResolution
	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"size",
		strconv.Itoa(width),
		strconv.Itoa(height),
		strconv.Itoa(dpi))); err != nil {
		return nil, err
	}

	// Send supported audio formats
	supportedAudios := info.AudioMimetypes

	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"audio", supportedAudios...)); err != nil {
		return nil, err
	}

	// Send supported video formats
	supportedVideos := info.VideoMimetypes

	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"video", supportedVideos...)); err != nil {
		return nil, err
	}

	// Send supported image formats
	supportedImages := info.ImageMimetypes
	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"image", supportedImages...)); err != nil {
		return nil, err
	}

	// Send client timezone, if supported and available
	clientTimezone := info.Timezone

	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"timezone", clientTimezone)); err != nil {
		return nil, err
	}

	// Send args
	if err = tunnel.WriteInstructionAndFlush(NewInstruction(
		"connect", connectArgsValues...)); err != nil {
		return nil, err
	}

	// Wait for ready, store ID
	ready, err := tunnel.expect("ready")
	if err != nil {
		return nil, err
	}

	if len(ready.Args) == 0 {
		err = errors.New("no connection id received")
		return nil, err
	}

	tunnel.uuid = ready.Args[0]
	tunnel.IsOpen = true
	tunnel.Config = config
	return tunnel, nil
}

type Tunnel struct {
	rw      *bufio.ReadWriter
	decoder *InstructionDecoder
	conn    net.Conn

	uuid   string
	Config Configuration
	IsOpen bool
}

func (t *Tunnel) UUID() string {
	return t.uuid
}

func (t *Tunnel) WriteInstructionAndFlush(instruction Instruction) (err error) {
	_, err = t.WriteAndFlush([]byte(instruction.String()))
	return
}

func (t *Tunnel) WriteInstruction(instruction Instruction) (err error) {
	_, err = t.Write([]byte(instruction.String()))
	return
}

func (t *Tunnel) WriteAndFlush(p []byte) (int, error) {
	nw, err := t.rw.Write(p)
	if err != nil {
		return nw, err
	}
	err = t.rw.Flush()
	if err != nil {
		return nw, err
	}
	return nw, nil
}

func (t *Tunnel) Write(p []byte) (int, error) {
	return t.rw.Write(p)
}

func (t *Tunnel) Flush() error {
	return t.rw.Flush()
}

func (t *Tunnel) ReadInstruction() (instruction Instruction, err error) {
	if err = t.conn.SetReadDeadline(time.Now().Add(defaultSocketTimeOut)); err != nil {
		return Instruction{}, err
	}
	if t.decoder == nil {
		t.decoder = NewInstructionDecoder(t.rw.Reader)
	}
	return t.decoder.ReadInstruction()
}

func (t *Tunnel) Read() ([]byte, error) {
	var (
		ins Instruction
		err error
	)
	if ins, err = t.ReadInstruction(); err != nil {
		return nil, err
	}
	return []byte(ins.String()), nil
}

func (t *Tunnel) expect(opcode string) (instruction Instruction, err error) {
	instruction, err = t.ReadInstruction()
	if err != nil {
		return instruction, err
	}

	if opcode != instruction.Opcode {
		msg := fmt.Sprintf(`expected "%s" instruction but instead received "%s"`, opcode, instruction.Opcode)
		return instruction, errors.New(msg)
	}
	return instruction, nil
}

func (t *Tunnel) Close() error {
	t.IsOpen = false
	return t.conn.Close()
}
