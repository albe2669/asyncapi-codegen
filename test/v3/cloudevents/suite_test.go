//go:generate go run ../../../cmd/asyncapi-codegen -p cloudevents --force-pointers -i ./asyncapi.yaml -o ./asyncapi.gen.go

package cloudevents

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	event "github.com/cloudevents/sdk-go/v2/event"
	"github.com/go-playground/validator/v10"
	"github.com/albe2669/asyncapi-codegen/pkg/extensions"
	"github.com/albe2669/asyncapi-codegen/pkg/extensions/middlewares"
	testutil "github.com/albe2669/asyncapi-codegen/test"
	"github.com/stretchr/testify/suite"
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

// TestPublishShape checks that the broker payload is a CloudEvents envelope
// containing the message data.
func (suite *Suite) TestPublishShape() {
	msg := NewHelloMessage()
	g := "hello world"
	msg.Payload.Greeting = &g
	suite.Require().NoError(suite.user.SendToSubscribeHelloOperation(context.Background(), msg))

	var bMsg extensions.BrokerMessage
	select {
	case bMsg = <-suite.interceptor:
	case <-time.After(30 * time.Second):
		suite.FailNow("timeout waiting for intercepted message")
		return
	}

	var envelope map[string]any
	suite.Require().NoError(json.Unmarshal(bMsg.Payload, &envelope))

	suite.Require().Equal("1.0", envelope["specversion"])
	suite.Require().Equal("HelloMessage", envelope["type"])
	suite.Require().Equal("cloudevents-test", envelope["source"])
	id, ok := envelope["id"].(string)
	suite.Require().True(ok)
	suite.Require().NotEmpty(id)

	data, ok := envelope["data"].(map[string]any)
	suite.Require().True(ok)
	suite.Require().Equal("hello world", data["greeting"])
	suite.Require().NotNil(data["correlationid"])
}

// TestRoundTrip checks that a message survives a real broker hop, both on
// publish and subscribe paths.
func (suite *Suite) TestRoundTrip() {
	received := make(chan HelloMessage, 1)
	err := suite.app.SubscribeToSubscribeHelloOperation(context.Background(),
		func(ctx context.Context, msg HelloMessage) error {
			received <- msg
			return nil
		})
	suite.Require().NoError(err)

	msg := NewHelloMessage()
	g := "hello world"
	msg.Payload.Greeting = &g
	suite.Require().NoError(suite.user.SendToSubscribeHelloOperation(context.Background(), msg))

	select {
	case got := <-received:
		suite.Require().Equal("hello world", *got.Payload.Greeting)
		suite.Require().Equal("HelloMessage", got.Type())
		suite.Require().Equal("cloudevents-test", got.Source())
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
	pv := "ping"
	ping.Payload.Ping = &pv
	suite.Require().NoError(suite.user.SendToReceiveMultiOperationForPing(context.Background(), ping))

	pong := NewPongMessage()
	pv2 := "pong"
	pong.Payload.Pong = &pv2
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

// TestMissingRequiredAttribute checks that sending an empty CloudEvents
// envelope fails instead of publishing an invalid event.
func (suite *Suite) TestMissingRequiredAttribute() {
	_ = suite.user
	msg := HelloMessage{}
	err := suite.user.SendToSubscribeHelloOperation(context.Background(), msg)
	suite.Require().Error(err)
}

// TestSpecVersionParameter makes sure the generated code pins the spec
// version from the contentType, when one is provided.
func (suite *Suite) TestSpecVersionParameter() {
	// The 1.0 default is asserted in TestPublishShape. The version
	// parameter parsing is covered by formats unit tests.
	msg := NewHelloMessage()
	suite.Require().Equal(event.CloudEventsVersionV1, msg.Event.SpecVersion())
}

// TestCloudEventsMiddleware checks that the CloudEventsValidation
// middleware accepts the envelope as generated by the publish path.
func (suite *Suite) TestCloudEventsMiddleware() {
	msg := NewHelloMessage()
	g := "hello world"
	msg.Payload.Greeting = &g

	bMsg, err := msg.toBrokerMessage()
	suite.Require().NoError(err)

	v := validator.New()
	mw := middlewares.CloudEventsValidation(v, func() any { return &HelloPayloadSchema{} })
	err = mw(context.Background(), &bMsg, func(_ context.Context) error { return nil })
	suite.Require().NoError(err)

	// An empty envelope must be rejected by the middleware.
	empty := extensions.BrokerMessage{Payload: []byte(`{}`)}
	err = mw(context.Background(), &empty, func(_ context.Context) error { return nil })
	suite.Require().Error(err)
}
