package milter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"net/textproto"
	"strings"
)

// OptAction sets which actions the milter wants to perform.
// Multiple options can be set using a bitmask.
type OptAction uint32

// OptProtocol masks out unwanted parts of the SMTP transaction.
// Multiple options can be set using a bitmask.
type OptProtocol uint32

const (
	// set which actions the milter wants to perform
	OptAddHeader    OptAction = 0x01
	OptChangeBody   OptAction = 0x02
	OptAddRcpt      OptAction = 0x04
	OptRemoveRcpt   OptAction = 0x08
	OptChangeHeader OptAction = 0x10
	OptQuarantine   OptAction = 0x20

	// mask out unwanted parts of the SMTP transaction
	OptNoConnect  OptProtocol = 0x01
	OptNoHelo     OptProtocol = 0x02
	OptNoMailFrom OptProtocol = 0x04
	OptNoRcptTo   OptProtocol = 0x08
	OptNoBody     OptProtocol = 0x10
	OptNoHeaders  OptProtocol = 0x20
	OptNoEOH      OptProtocol = 0x40
)

// MilterSession keeps session state during MTA communication
type MilterSession struct {
	headers  textproto.MIMEHeader
	macros   map[string]string
	ctx      context.Context
	milter   Milter
	actions  OptAction
	protocol OptProtocol
	sock     io.ReadWriteCloser
}

// ReadPacket reads incoming milter packet
func (c *MilterSession) ReadPacket() (*Message, error) {
	// read packet length
	var length uint32
	if err := binary.Read(c.sock, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	// read packet data
	data := make([]byte, length)
	if _, err := io.ReadFull(c.sock, data); err != nil {
		return nil, err
	}

	// prepare response data
	message := Message{
		Code: data[0],
		Data: data[1:],
	}

	return &message, nil
}

// WritePacket sends a milter response packet to socket stream
func (m *MilterSession) WritePacket(msg *Message) error {
	buffer := bufio.NewWriter(m.sock)

	// calculate and write response length
	length := uint32(len(msg.Data) + 1)
	if err := binary.Write(buffer, binary.BigEndian, length); err != nil {
		return err
	}

	// write response code
	if err := buffer.WriteByte(msg.Code); err != nil {
		return err
	}

	// write response data
	if _, err := buffer.Write(msg.Data); err != nil {
		return err
	}

	// flush data to network socket stream
	if err := buffer.Flush(); err != nil {
		return err
	}

	return nil
}

// Process processes incoming milter commands
func (m *MilterSession) Process(msg *Message) (Response, error) {
	switch msg.Code {
	case 'A':
		// abort current message and start over
		m.headers = nil
		m.macros = nil
		// do not send response
		return nil, nil

	case 'B':
		// body chunk
		return m.milter.BodyChunk(m.ctx, msg.Data, newModifier(m))

	case 'C':
		// new connection, get hostname
		Hostname := readCString(msg.Data)
		msg.Data = msg.Data[len(Hostname)+1:]
		// get protocol family
		protocolFamily := msg.Data[0]
		msg.Data = msg.Data[1:]
		// get port
		var Port uint16
		if protocolFamily == '4' || protocolFamily == '6' {
			if len(msg.Data) < 2 {
				return RespTempFail, nil
			}
			Port = binary.BigEndian.Uint16(msg.Data)
			msg.Data = msg.Data[2:]
		}
		// get address
		Address := readCString(msg.Data)
		// convert address and port to human readable string
		family := map[byte]string{
			'U': "unknown",
			'L': "unix",
			'4': "tcp4",
			'6': "tcp6",
		}
		// run handler and return
		return m.milter.Connect(
			m.ctx,
			Hostname,
			family[protocolFamily],
			Port,
			net.ParseIP(Address),
			newModifier(m))

	case 'D':
		// define macros
		m.macros = make(map[string]string)
		// convert data to Go strings
		data := decodeCStrings(msg.Data[1:])
		if len(data) != 0 {
			// store data in a map
			for i := 0; i < len(data); i += 2 {
				m.macros[data[i]] = data[i+1]
			}
		}
		// do not send response
		return nil, nil

	case 'E':
		// call and return milter handler
		return m.milter.Body(m.ctx, newModifier(m))

	case 'H':
		// helo command
		name := strings.TrimSuffix(string(msg.Data), null)
		return m.milter.Helo(m.ctx, name, newModifier(m))

	case 'L':
		// make sure headers is initialized
		if m.headers == nil {
			m.headers = make(textproto.MIMEHeader)
		}
		// add new header to headers map
		HeaderData := decodeCStrings(msg.Data)
		if len(HeaderData) == 2 {
			m.headers.Add(HeaderData[0], HeaderData[1])
			// call and return milter handler
			return m.milter.Header(m.ctx, HeaderData[0], HeaderData[1], newModifier(m))
		}

	case 'M':
		// envelope from address
		envfrom := readCString(msg.Data)
		return m.milter.MailFrom(m.ctx, strings.Trim(envfrom, "<>"), newModifier(m))

	case 'N':
		// end of headers
		return m.milter.Headers(m.ctx, m.headers, newModifier(m))

	case 'O':
		// ignore request and prepare response buffer
		buffer := new(bytes.Buffer)
		// prepare response data
		for _, value := range []uint32{2, uint32(m.actions), uint32(m.protocol)} {
			if err := binary.Write(buffer, binary.BigEndian, value); err != nil {
				return nil, err
			}
		}
		// build and send packet
		return NewResponse('O', buffer.Bytes()), nil

	case 'Q':
		// client requested session close
		return nil, eCloseSession

	case 'R':
		// envelope to address
		envto := readCString(msg.Data)
		return m.milter.RcptTo(m.ctx, strings.Trim(envto, "<>"), newModifier(m))

	case 'T':
		// data, ignore

	default:
		// print error and close session
		log.Printf("Unrecognized command code: %c", msg.Code)
		return nil, eCloseSession
	}

	// by default continue with next milter message
	return RespContinue, nil
}

//Serve serving session
func (m *MilterSession) Serve() error {
	defer m.sock.Close()

	for {
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
			// ReadPacket
			msg, err := m.ReadPacket()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			// process command
			resp, err := m.Process(msg)
			if err != nil {
				return err
			}

			// ignore empty responses
			if resp != nil {
				if err = m.WritePacket(resp.Response()); err != nil {
					return err
				}

				if !resp.Continue() {
					return nil
				}
			}
		}
	}
}
