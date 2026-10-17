package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrNoAIKey is an organization that keeps no AI key in HQ yet: an org
// Owner or Admin sets one in Mate (HQ answers 404 no_key).
var ErrNoAIKey = errors.New("the organization has no AI key yet")

// AIKey is the organization's key for a model provider, as HQ hands it to
// one of its Mates (@t3tools/shared/hqIntake HqMateAiKey): the provider, the
// variable apps read it from, the key, and the provider's OpenAI-compatible
// base URL. Value is a secret: never printed, logged or put in an error.
type AIKey struct {
	Provider string `json:"provider"`
	Env      string `json:"env"`
	Value    string `json:"value"`
	BaseURL  string `json:"baseUrl"`
}

// AIKey asks HQ for the organization's AI key, as the Mate. ErrNoAIKey when
// the organization keeps none; an answer missing a field is an error that
// never quotes the answer.
func (c Client) AIKey(ctx context.Context) (AIKey, error) {
	var key AIKey
	err := c.call.json(ctx, http.MethodPost, "/api/mate/ai-key", c.authorization(), map[string]string{}, &key)
	switch {
	case refusedAs(err, "no_key"):
		return AIKey{}, ErrNoAIKey
	case err != nil:
		return AIKey{}, fmt.Errorf("organization AI key: %w", err)
	case key.Provider == "" || key.Env == "" || key.Value == "" || key.BaseURL == "":
		return AIKey{}, errors.New("organization AI key: HQ's answer lacks the provider, its variable, the key or its base URL")
	}
	return key, nil
}
