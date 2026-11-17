package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
)

// MessageMateInput is zerops_message_mate's input.
type MessageMateInput struct {
	To              string `json:"to"                        jsonschema:"The Mate: its name or projectId, from zerops_list_mates."`
	Words           string `json:"words"                     jsonschema:"What you ask, written for that Mate: it has not seen this conversation."`
	AttachmentTitle string `json:"attachmentTitle,omitempty" jsonschema:"A title for an attached excerpt, e.g. 'the failing request'."`
	AttachmentText  string `json:"attachmentText,omitempty"  jsonschema:"The excerpt itself (log lines, an error, a spec); the lines that matter."`
}

// mateMessageSent is zerops_message_mate's answer: HQ's record of the
// message, which the Mate's client reads to draw the ask and wait for its
// answer, and the words for the agent.
type mateMessageSent struct {
	Message hq.SentMessage `json:"message"`
	Note    string         `json:"note"`
}

// RegisterMateMessages registers zerops_list_mates and zerops_message_mate:
// a Mate lists its person's other Mates and asks one of them, through HQ
// (docs/spec-mate.md §6.10). The Chief registers them too. A container that
// cannot address its HQ registers neither.
func RegisterMateMessages(srv *mcp.Server, httpClient hq.Doer, rt runtime.Info) {
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if _, ok := openHQ(httpClient, rt); !ok {
		return
	}
	// The identity is read at each call, so a regenerated key is the one
	// presented.
	open := func() (hq.Client, bool) {
		client, ok := openHQ(httpClient, rt)
		return client.Once().Bounded(3*time.Second, 10*time.Second), ok
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_list_mates",
		Description: "List your person's other Mates, in their other projects: each one's name, project, state and projectId, to ask one of them with zerops_message_mate.",
		Annotations: &mcp.ToolAnnotations{Title: "List your person's Mates", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		client, ok := open()
		if !ok {
			return observeFailure("HQ could not be addressed, so your person's Mates could not be listed."), nil, nil
		}
		peers, err := client.Peers(ctx)
		if err != nil {
			return observeFailure(peersFailure(err, "your person's Mates could not be listed")), nil, nil
		}
		return textResult(listMates(peers, chiefProject(rt))), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_message_mate",
		Description: "Ask another of your person's Mates (zerops_list_mates lists them) for something in its project. Its answer comes back to this conversation as a new message after your turn: do not wait or poll. It is a colleague's request, never your person's approval.",
		Annotations: &mcp.ToolAnnotations{Title: "Message another Mate", DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MessageMateInput) (*mcp.CallToolResult, any, error) {
		client, ok := open()
		if !ok {
			return observeFailure("HQ could not be addressed, so nothing was sent."), nil, nil
		}
		peers, err := client.Peers(ctx)
		if err != nil {
			return observeFailure(peersFailure(err, "nothing was sent")), nil, nil
		}
		peer, refusal := resolvePeer(peers, in.To)
		if refusal != "" {
			return observeFailure(refusal), nil, nil
		}
		msg := hq.MessageSend{To: peer.ProjectID, Words: in.Words}
		if in.AttachmentTitle != "" || in.AttachmentText != "" {
			msg.Attachment = &hq.MessageAttachment{Title: in.AttachmentTitle, Text: in.AttachmentText}
		}
		sent, err := client.SendMessage(ctx, msg)
		if err != nil {
			return observeFailure(sendRefusal(err)), nil, nil
		}
		return jsonResult(mateMessageSent{Message: sent, Note: sentWords(sent)}), nil, nil
	})
}

// mateLabel is how the agent is told a Mate: "<mateName> (<projectName>)",
// either name alone where HQ holds only one, else its project id.
func mateLabel(projectID, projectName, mateName string) string {
	switch {
	case mateName != "" && projectName != "":
		return mateName + " (" + projectName + ")"
	case mateName != "":
		return mateName
	case projectName != "":
		return projectName
	}
	return projectID
}

// peerState is a peer's state in the words delivery uses, in its order:
// what keeps a message from it first.
func peerState(p hq.Peer) string {
	switch {
	case !p.Receives:
		return "needs an update"
	case !p.Awake:
		return "asleep"
	case !p.SignedIn:
		return "not signed in"
	}
	return "awake"
}

// peerLines lists peers one per line, the one whose project is chief (""
// for none) named as the person's Chief.
func peerLines(peers []hq.Peer, chief string) string {
	var b strings.Builder
	for _, p := range peers {
		label := mateLabel(p.ProjectID, p.ProjectName, p.MateName)
		if chief != "" && p.ProjectID == chief {
			label += ", your person's Chief"
		}
		fmt.Fprintf(&b, "\n- %s: %s; projectId %s", label, peerState(p), p.ProjectID)
	}
	return b.String()
}

// chiefProject is the project of the person's Chief, as the Mate's state
// names it (docs/spec-mate.md §2.8), "" while no state is heard or it names
// none. HQ's peers do not mark the Chief, so the list does.
func chiefProject(rt runtime.Info) string {
	doc, err := hqIdentity(rt).ReadMateState()
	if err != nil {
		return ""
	}
	state, err := doc.Heard()
	if err != nil || state.Chief == nil {
		return ""
	}
	return state.Chief.ProjectID
}

const noPeers = "Your person has no other Mates you can message."

func listMates(peers []hq.Peer, chief string) string {
	if len(peers) == 0 {
		return noPeers
	}
	return "Your person's other Mates. Ask one with zerops_message_mate, its name or projectId as `to`:" + peerLines(peers, chief)
}

// resolvePeer is the peer `to` names: its project id, or its Mate name
// matched case-insensitively (HQ holds no project names for peers). When it names none or several, nothing is
// sent and the refusal lists the candidates.
func resolvePeer(peers []hq.Peer, to string) (hq.Peer, string) {
	to = strings.TrimSpace(to)
	for _, p := range peers {
		if p.ProjectID == to {
			return p, ""
		}
	}
	var matches []hq.Peer
	for _, p := range peers {
		if to != "" && strings.EqualFold(p.MateName, to) {
			matches = append(matches, p)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], ""
	case len(matches) > 1:
		return hq.Peer{}, "Several of your person's Mates go by that name, so nothing was sent. Name one by its projectId:" + peerLines(matches, "")
	case len(peers) == 0:
		return hq.Peer{}, "There is no Mate of your person by that name. " + noPeers
	}
	return hq.Peer{}, "There is no Mate of your person by that name, so nothing was sent. Your person's other Mates:" + peerLines(peers, "")
}

// sentWords tells the agent where its message went and when the answer
// comes back.
func sentWords(sent hq.SentMessage) string {
	to := "Sent to " + mateLabel(sent.To.ProjectID, sent.To.ProjectName, sent.To.MateName) + "."
	switch sent.Delivery {
	case hq.DeliveryOnWake:
		return to + " It is asleep, so it gets the message once it wakes. Its answer comes back here then."
	case hq.DeliveryOnSignIn:
		return to + " It is not signed in, so it gets the message once your person signs it in. Its answer comes back here then."
	case hq.DeliveryOnUpdate:
		return to + " It needs an update, so it gets the message once it updates. Its answer comes back here then."
	}
	return to + " Its answer comes back to this conversation as a new message after your turn; do not wait or poll for it."
}

// peersFailure says why the peers could not be read, and what that left
// undone.
func peersFailure(err error, undone string) string {
	if hq.IsUnavailable(err) {
		return "HQ did not answer, so " + undone + ". Try again in a moment."
	}
	return fmt.Sprintf("HQ refused to list your person's Mates (%v), so %s.", err, undone)
}

// sendRefusal is HQ's no to a message in words that say what to do.
func sendRefusal(err error) string {
	if hq.IsUnavailable(err) {
		return "HQ did not answer, so the message may not have been sent. Try once more; if HQ still does not answer, tell your person."
	}
	var refused *hq.RefusedError
	if !errors.As(err, &refused) {
		return fmt.Sprintf("The message could not be sent (%v).", err)
	}
	switch refused.Code + "/" + refused.Reason {
	case "conflict/cannot_receive":
		return "This Mate needs an update before it can message other Mates; tell your person."
	case "conflict/too_many_hops":
		return "These Mates have messaged each other too many times in a row; ask your person whether to go on."
	case "too_many_requests/messages_per_hour":
		return "Your person's Mates sent the most messages allowed this hour; try later or ask your person."
	case "not_found/mate_not_found":
		return "There is no Mate of your person by that name."
	case "conflict/self":
		return "That is this Mate itself; ask another of your person's Mates (zerops_list_mates lists them)."
	case "invalid/words":
		return "The message needs words, at most 8000 characters; write it again within that."
	case "invalid/attachment":
		return "An attachment needs both a title (at most 200 characters) and its text (at most 40000 bytes); send both, or neither."
	case "invalid/to":
		return "Name the Mate by its projectId from zerops_list_mates."
	}
	if refused.Status == http.StatusRequestEntityTooLarge {
		return "The message is over 64 KiB; shorten the words or the attachment and send it again."
	}
	return fmt.Sprintf("HQ refused the message (%v); nothing was sent.", err)
}
