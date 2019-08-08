// A Go library for milter support
package milter

import (
	"context"
	"net"
)

type MilterSessionOpt func(*MilterSession)

func WithContext(ctx context.Context) MilterSessionOpt {
	return func(ssn *MilterSession) {
		ssn.ctx = ctx
	}
}

func WithMilter(milter Milter) MilterSessionOpt {
	return func(ssn *MilterSession) {
		ssn.milter = milter
	}
}

func WithAction(action OptAction) MilterSessionOpt {
	return func(ssn *MilterSession) {
		ssn.actions |= action
	}
}

func WithProtocol(proto OptProtocol) MilterSessionOpt {
	return func(ssn *MilterSession) {
		ssn.protocol |= proto
	}
}

func NewMilterSession(conn net.Conn, opts ...MilterSessionOpt) *MilterSession {
	ssn := &MilterSession{
		sock: conn,
		ctx:  context.TODO(),
	}
	for _, opt := range opts {
		opt(ssn)
	}
	return ssn
}
