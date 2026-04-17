package messaging

import (
	"github.com/xdg-go/scram"
)

// HashGeneratorFcn is a type alias for the scram hash generator
type HashGeneratorFcn *scram.HashGeneratorFcn

// SHA256 is the SHA256 hash generator for SCRAM
var SHA256 HashGeneratorFcn = &scram.SHA256

// SHA512 is the SHA512 hash generator for SCRAM
var SHA512 HashGeneratorFcn = &scram.SHA512

// XDGSCRAMClient implements the sarama.SCRAMClient interface using xdg-go/scram
type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	HashGeneratorFcn HashGeneratorFcn
}

// Begin starts the SCRAM authentication conversation
func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = (*x.HashGeneratorFcn).NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

// Step processes a challenge from the server and returns the response
func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return response, err
}

// Done returns true if the authentication conversation is complete
func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
