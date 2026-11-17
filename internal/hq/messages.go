package hq

import (
	"context"
	"fmt"
	"net/http"
)

// A Mate messages its person's other Mates through HQ (docs/spec-mate.md
// §6.10): HQ lists them, stores each message, delivers it over the receiving
// Mate's link and sends the run's answer back the same way. zcp only lists
// and sends; the hop it lands on and the person it belongs to are HQ's.

// Peer is one of the caller's owner's other Mates in the organisation, as
// GET /api/mate/peers answers it.
type Peer struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	MateName    string `json:"mateName"`
	// Awake: its link to HQ is open now.
	Awake bool `json:"awake"`
	// Receives: its last overview advertised identity.messages.
	Receives bool `json:"receives"`
	// SignedIn: a login HQ records for it has the owner as its signer.
	SignedIn bool `json:"signedIn"`
}

// MessageAttachment is an excerpt a message carries beside its words.
type MessageAttachment struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// MessageSend is POST /api/mate/messages's body: To is the receiving Mate's
// project id.
type MessageSend struct {
	To         string             `json:"to"`
	Words      string             `json:"words"`
	Attachment *MessageAttachment `json:"attachment,omitempty"`
}

// MessageTo names the Mate a message went to.
type MessageTo struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	MateName    string `json:"mateName"`
}

// Deliveries a sent message can expect, as HQ names them.
const (
	DeliveryNow      = "now"
	DeliveryOnWake   = "on-wake"
	DeliveryOnSignIn = "on-sign-in"
	DeliveryOnUpdate = "on-update"
)

// SentMessage is HQ's record of a sent message: its id, the chain it joined
// and its hop there, whom it went to and when it gets delivered.
type SentMessage struct {
	ID       string    `json:"id"`
	ChainID  string    `json:"chainId"`
	Hop      int       `json:"hop"`
	To       MessageTo `json:"to"`
	Delivery string    `json:"delivery"`
}

// Peers lists the person's other Mates HQ lets this Mate message.
func (c Client) Peers(ctx context.Context) ([]Peer, error) {
	var answer struct {
		Mates []Peer `json:"mates"`
	}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/peers", c.authorization(), nil, &answer); err != nil {
		return nil, fmt.Errorf("list the person's Mates: %w", err)
	}
	return answer.Mates, nil
}

// SendMessage sends a message to another Mate of the person. HQ's refusals
// come back as *RefusedError, carrying its code and reason.
func (c Client) SendMessage(ctx context.Context, msg MessageSend) (SentMessage, error) {
	var sent SentMessage
	if err := c.call.json(ctx, http.MethodPost, "/api/mate/messages", c.authorization(), msg, &sent); err != nil {
		return SentMessage{}, fmt.Errorf("message a Mate: %w", err)
	}
	if sent.ID == "" {
		return SentMessage{}, fmt.Errorf("message a Mate: HQ returned no message id")
	}
	return sent, nil
}
