package server

import (
	"context"
	"io/ioutil"
	"logstore/internal/log/proto"
	log "logstore/internal/logcomponents"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func setupTest(t *testing.T, fn func(*Config)) (
	client proto.LogClient,
	config *Config,
	teardown func(),
) {
	t.Helper() //marks func as a helper (logs will be ignored)
	listener, err := net.Listen("tcp", ":0")
	assert.NoError(t, err)

	dialOps := []grpc.DialOption{grpc.WithInsecure()}
	clientConn, err := grpc.Dial(listener.Addr().String(), dialOps...)
	assert.NoError(t, err)

	dir, err := ioutil.TempDir("", "srv-test")
	assert.NoError(t, err)

	commitLog, err := log.NewLog(dir, log.Config{})
	assert.NoError(t, err)

	config = &Config{
		CommitLog: commitLog,
	}
	if fn != nil {
		fn(config)
	}

	srv, err := NewGRPCServer(config)
	assert.NoError(t, err)

	go func() {
		srv.Serve(listener)
	}()

	client = proto.NewLogClient(clientConn)

	return client, config, func() { //returning an anon func that shutsdown srv
		srv.Stop()
		clientConn.Close()
		listener.Close()
		commitLog.Remove()
	}
}

func TestServer(t *testing.T) {
	for scenario, fn := range map[string]func(
		t *testing.T,
		client proto.LogClient,
		config *Config,
	){
		"unary success":      testUnaryAppendRead,
		"stream success":     testStreamAppendRead,
		"read out of bounds": testOOBRead,
	} {
		t.Run(scenario, func(t *testing.T) {
			client, config, teardown := setupTest(t, nil)
			defer teardown()
			fn(t, client, config)
		})
	}
}

func testUnaryAppendRead(
	t *testing.T,
	client proto.LogClient,
	config *Config,
) {
	ctx := context.Background()

	expected := &proto.Record{
		Value: []byte("record"),
	}
	appReq := &proto.AppendRequest{
		Record: expected,
	}
	append, err := client.Append(ctx, appReq)
	assert.NoError(t, err)

	readReq := &proto.ReadRequest{
		Offset: append.Offset,
	}
	read, err := client.Read(ctx, readReq)
	assert.NoError(t, err)
	assert.Equal(t, expected.Value, read.Record.Value)
	assert.Equal(t, expected.Offset, read.Record.Offset)
}

func testStreamAppendRead(
	t *testing.T,
	client proto.LogClient,
	config *Config,
) {
	ctx := context.Background()

	records := []*proto.Record{
		{
			Value:  []byte("uno"),
			Offset: 0,
		}, {
			Value:  []byte("dos"),
			Offset: 1,
		},
	}

	{
		stream, err := client.AppendStream(ctx)
		assert.NoError(t, err)

		for offset, record := range records {
			apdReq := &proto.AppendRequest{
				Record: record,
			}
			err = stream.Send(apdReq)
			assert.NoError(t, err)

			res, err := stream.Recv()
			assert.NoError(t, err)
			if res.Offset != uint64(offset) {
				t.Fatalf(
					"actual offset: %d, expected %d",
					res.Offset,
					offset,
				)
			}
		}
	}

	{
		readReq := &proto.ReadRequest{Offset: 0}
		stream, err := client.ReadStream(ctx, readReq)
		assert.NoError(t, err)

		for i, record := range records {
			res, err := stream.Recv()

			assert.NoError(t, err)
			expected := &proto.Record{
				Value:  record.Value,
				Offset: uint64(i),
			}
			assert.Equal(t, res.Record, expected)
		}
	}
}

func testOOBRead(
	t *testing.T,
	client proto.LogClient,
	config *Config,
) {
	ctx := context.Background()

	record := &proto.Record{
		Value: []byte("record"),
	}
	appReq := &proto.AppendRequest{
		Record: record,
	}
	append, err := client.Append(ctx, appReq)
	assert.NoError(t, err)

	readReq := &proto.ReadRequest{
		Offset: append.Offset + 1,
	}
	read, err := client.Read(ctx, readReq)
	if read != nil {
		t.Error("read not nil")
	}

	expected := grpc.Code(proto.ErrOffOutOfRange{}.GRPCStatus().Err())
	actual := grpc.Code(err)
	if actual != expected {
		t.Fatalf("actual err: %v, expected: %v", actual, expected)
	}

}