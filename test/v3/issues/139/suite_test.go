//go:generate go run ../../../../cmd/asyncapi-codegen -p issue139 -i ./asyncapi.yaml -o ./asyncapi.gen.go

package issue139

import (
	"context"
	"testing"

	"github.com/albe2669/asyncapi-codegen/pkg/extensions"
	"github.com/albe2669/asyncapi-codegen/pkg/utils"
	"github.com/stretchr/testify/suite"
)

func TestSuite(t *testing.T) {
	suite.Run(t, NewSuite())
}

// capturingBroker records the channel address of the last published message.
type capturingBroker struct {
	publishedChannel string
}

func (b *capturingBroker) Publish(_ context.Context, channel string, _ extensions.BrokerMessage) error {
	b.publishedChannel = channel
	return nil
}

func (b *capturingBroker) Subscribe(
	_ context.Context,
	_ string,
) (extensions.BrokerChannelSubscription, error) {
	return extensions.NewBrokerChannelSubscription(make(chan extensions.AcknowledgeableBrokerMessage), make(chan any)), nil
}

type Suite struct {
	suite.Suite
}

func NewSuite() *Suite {
	return &Suite{}
}

// TestParameterFilledFromMessageLocation ensures a channel parameter that
// declares a `location` is auto-filled from the outgoing message when the
// caller leaves it empty (issue #139).
func (suite *Suite) TestParameterFilledFromMessageLocation() {
	broker := &capturingBroker{}
	ctrl, err := NewAppController(broker)
	suite.Require().NoError(err)
	defer ctrl.Close(context.Background())

	msg := NewUserSignedUpMessage()
	msg.Payload.UserId = utils.ToPointer("abc")

	// userId left empty in params: it must be filled from the message payload.
	err = ctrl.SendAsSendUserOperation(context.Background(), UserChannelParameters{}, msg)
	suite.Require().NoError(err)
	suite.Require().Equal("v3.issue139.user.abc", broker.publishedChannel)
}

// TestParameterMessageMismatchRejected ensures an explicitly provided parameter
// that contradicts the message payload fails the send instead of silently
// routing the message against its own data.
func (suite *Suite) TestParameterMessageMismatchRejected() {
	broker := &capturingBroker{}
	ctrl, err := NewAppController(broker)
	suite.Require().NoError(err)
	defer ctrl.Close(context.Background())

	msg := NewUserSignedUpMessage()
	msg.Payload.UserId = utils.ToPointer("abc")

	err = ctrl.SendAsSendUserOperation(context.Background(), UserChannelParameters{UserId: "override"}, msg)
	suite.Require().ErrorIs(err, extensions.ErrParameterMessageMismatch)
	suite.Require().Empty(broker.publishedChannel)
}
