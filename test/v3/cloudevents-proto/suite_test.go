//go:generate go run ../../../cmd/asyncapi-codegen -p cloudeventsproto --force-pointers -i ./asyncapi.yaml -o ./asyncapi.gen.go

package cloudeventsproto

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/albe2669/asyncapi-codegen/pkg/extensions"
	"github.com/albe2669/asyncapi-codegen/pkg/extensions/middlewares"
	testutil "github.com/albe2669/asyncapi-codegen/test"
	hellov1 "github.com/albe2669/asyncapi-codegen/test/v3/cloudevents-proto/hello/v1"
	pb "github.com/albe2669/asyncapi-codegen/test/v3/cloudevents-proto/io/cloudevents/v1"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/proto"
)

func TestSuite(t *testing.T) {
	brokers, cleanup := testutil.BrokerControllers(t)
	defer cleanup()

	for _, b := range brokers {
		suite.Run(t, NewSuite(b))
	}
}

type Suite struct {
	broker      extensions.BrokerController
	app         *AppController
	user        *UserController
	interceptor chan extensions.BrokerMessage
	suite.Suite
}

func NewSuite(broker extensions.BrokerController) *Suite {
	return &Suite{broker: broker}
}

func (suite *Suite) SetupTest() {
	suite.interceptor = make(chan extensions.BrokerMessage, 8)

	app, err := NewAppController(suite.broker, WithMiddlewares(middlewares.Intercepter(suite.interceptor)))
	suite.Require().NoError(err)
	suite.app = app

	user, err := NewUserController(suite.broker, WithMiddlewares(middlewares.Intercepter(suite.interceptor)))
	suite.Require().NoError(err)
	suite.user = user
}

func (suite *Suite) TearDownTest() {
	suite.app.Close(context.Background())
	suite.user.Close(context.Background())
}

// TestPublishShape checks that the broker payload is a CloudEvents protobuf
// envelope carrying the typed payload in proto_data.
func (suite *Suite) TestPublishShape() {
	msg := NewHelloMessage()
	msg.Payload.Greeting = "hello world"
	suite.Require().NoError(suite.user.SendToSubscribeHelloOperation(context.Background(), msg))

	var bMsg extensions.BrokerMessage
	select {
	case bMsg = <-suite.interceptor:
	case <-time.After(30 * time.Second):
		suite.FailNow("timeout waiting for intercepted message")
		return
	}

	var envelope pb.CloudEvent
	suite.Require().NoError(proto.Unmarshal(bMsg.Payload, &envelope))

	suite.Require().Equal("1.0", envelope.SpecVersion)
	suite.Require().Equal("HelloMessage", envelope.Type)
	suite.Require().Equal("cloudevents-proto-test", envelope.Source)
	suite.Require().NotEmpty(envelope.Id)

	protoData, ok := envelope.Data.(*pb.CloudEvent_ProtoData)
	suite.Require().True(ok, "envelope must carry the payload in proto_data")

	var wirePayload hellov1.HelloPayload
	suite.Require().NoError(protoData.ProtoData.UnmarshalTo(&wirePayload))
	suite.Require().Equal("hello world", wirePayload.Greeting)
}

// TestRoundTrip checks that a typed proto message survives a real broker hop
// on both publish and subscribe paths, with correlation id intact.
func (suite *Suite) TestRoundTrip() {
	received := make(chan HelloMessage, 1)
	err := suite.app.SubscribeToSubscribeHelloOperation(context.Background(),
		func(ctx context.Context, msg HelloMessage) error {
			received <- msg
			return nil
		})
	suite.Require().NoError(err)

	msg := NewHelloMessage()
	msg.Payload.Greeting = "hello world"
	suite.Require().NoError(suite.user.SendToSubscribeHelloOperation(context.Background(), msg))

	select {
	case got := <-received:
		suite.Require().Equal("hello world", got.Payload.Greeting)
		suite.Require().Equal("HelloMessage", got.Type)
		suite.Require().Equal("cloudevents-proto-test", got.Source)
		suite.Require().NotEmpty(got.CorrelationID())
	case <-time.After(30 * time.Second):
		suite.FailNow("timeout waiting for message")
	}
}

// TestMultiMessageDispatch checks that the right callback fires for each
// message on a channel with multiple messages.
func (suite *Suite) TestMultiMessageDispatch() {
	var mu sync.Mutex
	var gotPings, gotPongs int

	err := suite.app.SubscribeToReceiveMultiOperationForPing(context.Background(),
		func(ctx context.Context, msg PingMessage) error {
			mu.Lock()
			defer mu.Unlock()
			gotPings++
			return nil
		})
	suite.Require().NoError(err)

	err = suite.app.SubscribeToReceiveMultiOperationForPong(context.Background(),
		func(ctx context.Context, msg PongMessage) error {
			mu.Lock()
			defer mu.Unlock()
			gotPongs++
			return nil
		})
	suite.Require().NoError(err)

	ping := NewPingMessage()
	ping.Payload.Ping = "ping"
	suite.Require().NoError(suite.user.SendToReceiveMultiOperationForPing(context.Background(), ping))

	pong := NewPongMessage()
	pong.Payload.Pong = "pong"
	suite.Require().NoError(suite.user.SendToReceiveMultiOperationForPong(context.Background(), pong))

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		pings, pongs := gotPings, gotPongs
		mu.Unlock()
		if pings == 1 && pongs == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	suite.Require().Equal(1, gotPings)
	suite.Require().Equal(1, gotPongs)
}

// TestConstructorDefaults verifies that NewHelloMessage fills the CloudEvents
// envelope attributes per the format snippets.
func (suite *Suite) TestConstructorDefaults() {
	msg := NewHelloMessage()

	suite.Require().Equal("1.0", msg.SpecVersion)
	suite.Require().NotEmpty(msg.Id)
	suite.Require().Equal("cloudevents-proto-test", msg.Source)
	suite.Require().Equal("HelloMessage", msg.Type)
	suite.Require().NotEmpty(msg.CorrelationID())
}

// TestTypedPayload asserts that the message payload is the exact proto
// generated struct type: the strong-typing guarantee of the format.
func (suite *Suite) TestTypedPayload() {
	var msg HelloMessage
	var payload hellov1.HelloPayload
	msg.Payload = payload
	msg.Payload.Greeting = "typed"

	suite.Require().Equal("typed", msg.Payload.Greeting)

	// Assignability verifies the field is the exact buf-generated type.
	var asAny any = msg.Payload
	_, ok := asAny.(hellov1.HelloPayload)
	suite.Require().True(ok)
}
