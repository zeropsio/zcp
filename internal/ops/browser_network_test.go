package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"
)

// witness: internal/ops/browser_warm.go | if browserNetworkRequests(step) { → if isCommand(step, browserCmdErrors) {
func TestBrowserNetwork_BNHeaders_RedactsCredentialHeaders(t *testing.T) {
	t.Parallel()
	const input = `{"requests":[{"url":"https://example.test/private","method":"POST","status":401,"timing":{"duration":8.25},"headers":{"Authorization":"request-auth","Cookie":"request-cookie","X-Access-TOKEN":"request-token","Content-Type":"application/json"},"responseHeaders":{"authorization":"response-auth","cookie":"response-cookie","Set-Cookie":"response-set-cookie","Proxy-Authorization":"response-proxy","X-Client-Secret":"response-secret","X-Api-Key":"response-key","Content-Type":"text/plain"}},{"url":"https://example.test/error","method":"GET","status":503,"timing":{"duration":12}}]}`
	const redacted = `{"requests":[{"url":"https://example.test/private","method":"POST","status":401,"timing":{"duration":8.25},"headers":{"Authorization":"<redacted>","Cookie":"<redacted>","X-Access-TOKEN":"<redacted>","Content-Type":"application/json"},"responseHeaders":{"authorization":"<redacted>","cookie":"<redacted>","Set-Cookie":"<redacted>","Proxy-Authorization":"<redacted>","X-Client-Secret":"<redacted>","X-Api-Key":"<redacted>","Content-Type":"text/plain"}},{"url":"https://example.test/error","method":"GET","status":503,"timing":{"duration":12}}]}`
	const listInput = `[{"url":"https://example.test/","method":"GET","status":403,"headers":[{"name":"Cookie","value":"request-cookie"},{"name":"Accept","value":"text/html"},{"key":"Proxy-Authorization","value":"proxy-value"},{"name":"X-Secret","raw":"secret-value"},["X-Api-Key","key-value"]],"responseHeaders":[{"name":"set-cookie","value":"response-cookie"}]}]`
	const listRedacted = `[{"url":"https://example.test/","method":"GET","status":403,"headers":[{"name":"Cookie","value":"<redacted>"},{"name":"Accept","value":"text/html"},{"key":"Proxy-Authorization","value":"<redacted>"},{"name":"X-Secret","raw":"<redacted>"},["X-Api-Key","<redacted>"]],"responseHeaders":[{"name":"set-cookie","value":"<redacted>"}]}]`
	for _, tt := range []struct {
		name       string
		command    []string
		input      string
		want       string
		runErr     error
		projection bool
	}{
		{"closing step", []string{"network", "requests", "--status", "400-599"}, input, redacted, nil, true},
		{"partial walk", []string{"network", "requests"}, input, redacted, context.DeadlineExceeded, false},
		{"global flag and header list", []string{"--json", "network", "requests"}, listInput, listRedacted, nil, false},
		{"other step", []string{"snapshot"}, input, input, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			steps := [][]string{{"errors"}, {"console"}, tt.command}
			stdout, _, _, err := runBrowserSteps(context.Background(), steps, func(_ context.Context, cmd []string) (string, string, error) {
				if slices.Equal(cmd, tt.command) {
					return `{"success":true,"data":` + tt.input + `}`, "", nil
				}
				return `{"success":true,"data":{}}`, "", nil
			})
			if err != nil {
				t.Fatalf("walk: %v", err)
			}
			var result BrowserBatchResult
			done, _ := classifyRunResult(&result, stdout, "", tt.runErr, time.Second)
			if len(result.Steps) != len(steps) {
				t.Fatalf("got %d steps, want %d", len(result.Steps), len(steps))
			}
			var got, want any
			if err := json.Unmarshal(result.Steps[2].Result, &got); err != nil {
				t.Fatalf("decode result: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatalf("decode expectation: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("step result = %s, want %s", result.Steps[2].Result, tt.want)
			}
			if tt.projection {
				if done {
					t.Fatal("completed walk was treated as partial")
				}
				populateBrowserBatchOutputs(&result, false, "")
				want := []NetworkRequest{{URL: "https://example.test/private", Method: "POST", Status: 401}, {URL: "https://example.test/error", Method: "GET", Status: 503}}
				if !reflect.DeepEqual(result.NetworkOutput, want) {
					t.Errorf("failed requests = %+v, want %+v", result.NetworkOutput, want)
				}
			}
		})
	}
}
