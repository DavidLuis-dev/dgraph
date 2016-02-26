package conn

import (
	"bytes"
	"net/rpc"
	"testing"
)

type buf struct {
	data chan byte
}

func newBuf() *buf {
	b := new(buf)
	b.data = make(chan byte, 10000)
	return b
}

func (b *buf) Read(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		p[i] = <-b.data
	}
	return len(p), nil
}

func (b *buf) Write(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		b.data <- p[i]
	}
	return len(p), nil
}

func (b *buf) Close() error {
	close(b.data)
	return nil
}

func TestWriteAndParseHeader(t *testing.T) {
	b := newBuf()
	data := []byte("oh hey")
	if err := writeHeader(b, 11, "testing.T", data); err != nil {
		t.Error(err)
		t.Fail()
	}
	var seq uint64
	var method string
	var plen int32
	if err := parseHeader(b, &seq, &method, &plen); err != nil {
		t.Error(err)
		t.Fail()
	}
	if seq != 11 {
		t.Error("Sequence number. Expected 11. Got: %v", seq)
	}
	if method != "testing.T" {
		t.Error("Method name. Expected: testing.T. Got: %v", method)
	}
	if plen != int32(len(data)) {
		t.Error("Payload length. Expected: %v. Got: %v", len(data), plen)
	}
}

func TestClientToServer(t *testing.T) {
	b := newBuf()
	cc := &ClientCodec{
		Rwc: b,
	}
	sc := &ServerCodec{
		Rwc: b,
	}

	r := &rpc.Request{
		ServiceMethod: "Test.ClientServer",
		Seq:           11,
	}

	query := new(Query)
	query.Data = []byte("iamaquery")
	if err := cc.WriteRequest(r, query); err != nil {
		t.Error(err)
	}

	sr := new(rpc.Request)
	if err := sc.ReadRequestHeader(sr); err != nil {
		t.Error(err)
	}
	if sr.Seq != r.Seq {
		t.Error("RPC Seq. Expected: %v. Got: %v", r.Seq, sr.Seq)
	}
	if sr.ServiceMethod != r.ServiceMethod {
		t.Error("ServiceMethod. Expected: %v. Got: %v",
			r.ServiceMethod, sr.ServiceMethod)
	}

	squery := new(Query)
	if err := sc.ReadRequestBody(squery); err != nil {
		t.Error(err)
	}
	if !bytes.Equal(squery.Data, query.Data) {
		t.Error("Queries don't match. Expected: %v Got: %v",
			string(query.Data), string(squery.Data))
	}
}

func TestServerToClient(t *testing.T) {
	b := newBuf()
	cc := &ClientCodec{
		Rwc: b,
	}
	sc := &ServerCodec{
		Rwc: b,
	}

	r := &rpc.Response{
		ServiceMethod: "Test.ClientServer",
		Seq:           11,
	}

	reply := new(Reply)
	reply.Data = []byte("iamareply")
	if err := sc.WriteResponse(r, reply); err != nil {
		t.Error(err)
	}

	cr := new(rpc.Response)
	if err := cc.ReadResponseHeader(cr); err != nil {
		t.Error(err)
	}
	if cr.Seq != r.Seq {
		t.Error("RPC Seq. Expected: %v. Got: %v", r.Seq, cr.Seq)
	}
	if cr.ServiceMethod != r.ServiceMethod {
		t.Error("ServiceMethod. Expected: %v. Got: %v",
			r.ServiceMethod, cr.ServiceMethod)
	}

	creply := new(Reply)
	if err := cc.ReadResponseBody(creply); err != nil {
		t.Error(err)
	}
	if !bytes.Equal(creply.Data, reply.Data) {
		t.Error("Replies don't match. Expected: %v Got: %v",
			string(reply.Data), string(creply.Data))
	}
}
