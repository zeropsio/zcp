package hq

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Check is a GitHub check run as HQ names it.
type Check struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Conclusion *string `json:"conclusion"`
	URL        string  `json:"url"`
}

// ChecksAnswer is the failing checks on a change's head.
type ChecksAnswer struct {
	Head        *string           `json:"head"`
	Checks      []Check           `json:"checks"`
	ReviewBuild *CheckReviewBuild `json:"reviewBuild,omitempty"`
}

// CheckReviewBuild is the failed Review build HQ names in its checks answer.
type CheckReviewBuild struct {
	Head  string            `json:"head"`
	Parts []CheckReviewPart `json:"parts"`
}

// CheckReviewPart points to the platform's version or the health observation.
type CheckReviewPart struct {
	Setup        string            `json:"setup"`
	Stage        string            `json:"stage"`
	Outcome      string            `json:"outcome"`
	Phase        string            `json:"phase,omitempty"`
	AppVersionID string            `json:"appVersionId,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Probe        *CheckReviewProbe `json:"probe,omitempty"`
}

// CheckReviewProbe is the observation made for a public stage.
type CheckReviewProbe struct {
	URL    string `json:"url"`
	Status *int   `json:"status,omitempty"`
	At     string `json:"at"`
}

// CheckLogQuery holds only the bounds and pattern the agent supplied.
type CheckLogQuery struct {
	From  *int64
	To    *int64
	Match *string
}

// CheckLogEntry is a numbered line, marked when HQ cut it.
type CheckLogEntry struct {
	N    int    `json:"n"`
	Text string `json:"text"`
	Cut  bool   `json:"cut"`
}

// CheckLogAnswer is HQ's bounded selection from a check's log.
type CheckLogAnswer struct {
	Check    Check           `json:"check"`
	Head     *string         `json:"head"`
	Lines    int             `json:"lines"`
	Whole    bool            `json:"whole"`
	Selected int             `json:"selected"`
	Entries  []CheckLogEntry `json:"entries"`
}

// FailingChecks reads the Mate's open change's failing checks from HQ.
func (c Client) FailingChecks(ctx context.Context, repo string, number int) (ChecksAnswer, error) {
	var answer ChecksAnswer
	err := c.call.json(ctx, http.MethodGet, changePath(repo, number)+"/checks", c.authorization(), nil, &answer)
	return answer, err
}

// CheckLog reads one check's log with only the query fields supplied.
func (c Client) CheckLog(ctx context.Context, repo string, number int, check int64, query CheckLogQuery) (CheckLogAnswer, error) {
	values := url.Values{}
	if query.From != nil {
		values.Set("from", strconv.FormatInt(*query.From, 10))
	}
	if query.To != nil {
		values.Set("to", strconv.FormatInt(*query.To, 10))
	}
	if query.Match != nil {
		values.Set("match", *query.Match)
	}
	path := changePath(repo, number) + "/checks/" + strconv.FormatInt(check, 10) + "/log"
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	var answer CheckLogAnswer
	err := c.call.json(ctx, http.MethodGet, path, c.authorization(), nil, &answer)
	return answer, err
}
